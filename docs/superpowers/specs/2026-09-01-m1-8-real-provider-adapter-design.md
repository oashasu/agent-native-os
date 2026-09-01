# M1.8 — Agent Adapter: Real Provider #1 (codex) — Design

**Status:** for review (2026-09-01)
**Spec source:** `docs/M1-DESIGN.md` §8 (真实 Harness Adapter — provider-neutral, runtime discovery, dual-track), §6 (`AgentRun` data model), §13 (milestone M1.8). Also `docs/superpowers/plans/2026-08-29-m1-3-agent-adapter.md` (which deferred "actually interrupt the in-flight goroutine" to M1.8).
**Milestone position:** after M1.7 (`m1.7-done-integrity-qualification`, main `621871c`), before M1.9 (full §10 qualification with the real provider).

---

## 1. Goal

Give `org.vibe.agent.harness` a **`RealProvider`** — a subprocess-driven implementation of the existing `Provider` interface — with **`codex`** as real provider #1, discovered at plugin startup and selectable per run. The `mock` provider stays the default everywhere, so `smoke.sh` and `qualify-done-integrity.sh` remain deterministic. `agent.run.cancel` is upgraded from a store-only status flip to a real interruption of the running provider (deferred here from M1.3).

The real end-to-end path (a real `codex` making a real change) is verified **only locally** by a credential-gated manual script — the air-gapped dispatch sandbox cannot run it. The dispatched work is the deterministic plumbing, exercised through a committed fake-CLI fixture.

## 2. Design invariants (must hold at merge)

0. **Toolchain: Go 1.19.** `plugins/go.mod` / `cli/go.mod` say `go 1.19`; the dev machine is go1.19.1. No Go 1.20+ API (`exec.Cmd.Cancel`, `exec.Cmd.WaitDelay`, `errors.Join`, `context.WithoutCancel`, …). No implicit toolchain bump — if a task seems to need one, stop and report.
1. **Provider-neutral protocol.** `agent.run@1` / `workflow.engineering.run@1` gain nothing provider-specific. No `CodexThread` / `ClaudeConversation` vocabulary anywhere in contracts or handlers.
2. **`provider` is a selection hint only.** It names which registered provider to use. It never carries an executable path, argv, environment, or credentials. Those are server-internal.
3. **Empty `provider` resolves to `mock`. Unknown `provider` returns `INVALID` *before* `RecordStarted`** (no orphan RUNNING record).
4. **`RealProvider` does not parse codex event semantics.** codex stdout (even with `--json`) is treated as opaque lines. No codex field becomes a business field.
5. **Deterministic status mapping.** context-cancel → `CANCELLED`; context-deadline → `TIMEOUT`; non-zero exit → `FAILED`; clean exit → `COMPLETED`; `cmd.Start()` failure → `FAILED` with `exit_code: null`.
6. **Mock's `mock_*` parameters never influence `RealProvider`.**
7. **Live cancel terminates the provider first, then writes the terminal record** (with the partial transcript).
8. **`raw_session_ref`** is the *unparsed, line-by-line text transcript* the adapter assembled (frames + a result line), **not** a byte-exact copy of the child's stdout/stderr streams and not their true interleaving.
9. **Real-provider metadata is redacted.** For a `RealProvider` invocation, `AgentRun.ProviderMetadata` may contain only the provider name and exit code. The executable path, the full derived argv, allowlisted environment values, and credentials must not be copied into `provider_metadata` or any extra RPC field. The existing `AgentRun.Prompt` field remains unchanged; this rule forbids duplicating the prompt inside command metadata, not the established prompt field itself.

## 3. RPC boundary

| Field | Origin | Ownership |
|---|---|---|
| `agent.run` request `.provider` | caller, optional | provider selection hint |
| `workflow.engineering.run` request `.provider` | caller, optional | passed straight through to `agent.run` |
| default provider | server | `mock` |
| executable path, argv, env, credentials | server-internal (`discovery.go` / `RealProvider`) | never on the wire |
| `provider_metadata` | server-derived, redacted for `RealProvider` | only provider name + exit code for real runs; never executable path, full argv, env, or credentials. Existing mock metadata remains unchanged. |
| raw transcript | provider-internal | returned only via the existing `raw_session_ref` blob URI |
| `harness_native_id` | server-synthesised: `<provider>-<run_id>` | adapter's id for this invocation — **not** claimed to be codex's native thread id |

## 4. Components

### 4.1 `Provider` interface — unchanged

```go
type Provider interface {
	Name() string
	Run(ctx context.Context, spec RunSpec, out chan<- Frame) RunResult
}
```

`RunSpec` gains nothing required by `RealProvider` (it already has `Prompt`, `WorkspacePath`). The `Mock*` fields stay; `RealProvider` ignores them. `RunResult.NativeID` is **unused by `RealProvider`** (returns `""`); the handler synthesises `harness_native_id`.

### 4.2 `RealProvider` (`plugins/agent-harness/real_provider.go` + platform files)

**Toolchain: Go 1.19.** `plugins/go.mod` and `cli/go.mod` declare `go 1.19`; the dev machine has go1.19.1. **No Go 1.20+ API** — in particular `exec.Cmd.Cancel` and `exec.Cmd.WaitDelay` do not exist. Use `exec.Command` + `Start` + a manual wait/kill loop.

```go
type RealProvider struct {
	name    string                     // "codex"
	bin     string                     // absolute path from exec.LookPath
	argv    func(spec RunSpec) []string // args AFTER the binary
	env     []string                   // pre-computed allowlisted env; NEVER nil (nil => inherit full parent env)
	timeout time.Duration              // 0 = no adapter-imposed deadline (caller's ctx still applies)
}
```

`RealProvider.Name()` returns `p.name`; constructors and discovery must reject an empty name, binary, or argv template rather than allowing a nil function or empty executable to panic later.

Process spawn + group kill live in platform files so a non-POSIX build still compiles. Go 1.19 supplies the built-in `unix` build tag for Unix-like targets; use `unix`/`!unix` consistently in the source and test constraints.
- `real_provider_exec_unix.go` (`//go:build unix`): `realProviderSupported() == true`; `startProcess(cmd)` sets `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}` then `cmd.Start()`; `killProcessGroup(cmd)` = `syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)`.
- `real_provider_exec_other.go` (`//go:build !unix`): `realProviderSupported() == false`; `startProcess` returns an error `"real provider unsupported on this platform"`; `killProcessGroup` = `cmd.Process.Kill()`. (`discoverProviders` checks `realProviderSupported()` and registers no real providers on unsupported platforms.)

`Run(ctx, spec, out)`:

1. `defer close(out)`.
2. Reject an empty `p.name`, empty `p.bin`, nil `p.argv`, or nil `p.env` with `StatusFailed`/`exit_code: null`; then compute `args := p.argv(spec)` (§5 for codex). This guard prevents a malformed direct construction from panicking or inheriting the parent environment.
3. Define `runCtx := ctx`. If `p.timeout > 0`, declare `var cancel context.CancelFunc`, then assign `runCtx, cancel = context.WithTimeout(ctx, p.timeout)` and `defer cancel()`. Every later context check and cancellation select uses `runCtx`, not the original `ctx`. If `runCtx.Err()` is already non-nil, return the corresponding `CANCELLED`/`TIMEOUT` result with `exit_code: null` before spawning a process.
4. `cmd := exec.Command(p.bin, args...)`; `cmd.Dir = spec.WorkspacePath`; `cmd.Env = p.env` (the implementation must reject or normalize a nil env before spawning; nil would inherit the parent environment).
5. Create two ordinary OS pipes with `os.Pipe()` and assign the write ends directly to `cmd.Stdout` / `cmd.Stderr`; retain the read ends for the two reader goroutines. Do **not** use `Cmd.StdoutPipe` / `Cmd.StderrPipe`: in Go 1.19 `Cmd.Wait` closes those parent read ends, and calling `Wait` only after readers finish can hang forever if a descendant inherits a pipe. Check every pipe-creation error, close all ends already created on failure, and return `RunResult{Status: StatusFailed, ProviderMeta: meta(p, nil)}`. After a successful `startProcess(cmd)`, close the parent copies of both write ends immediately. A start error closes all four ends and returns the same result (`exit_code` JSON `null`). `meta` never serializes the command arguments.
6. Two reader goroutines (one per read end). Create an idempotent `stopReaders` helper backed by `sync.Once`; it closes a shared `readerStop` channel and both read ends exactly once. Each reader uses `bufio.Reader.ReadLine` — **not `ReadString`**, which can allocate an unbounded logical line — and handles `isPrefix` fragments:
   - Keep an accumulator capped at `maxFrameText = 1 MiB`; the accumulator itself must never exceed that bound.
   - For a line exceeding the bound, emit one frame containing the first `maxFrameText-len("…[truncated]")` bytes plus `"…[truncated]"`, so the final `Text` is at most 1 MiB; discard remaining fragments through the line terminator and continue.
   - Normalize one trailing `\r` from a completed line. On `io.EOF` with non-empty residual (no trailing newline) → emit that residual as a final frame.
   - On a non-EOF read error → emit one frame with the source stream's `Kind` and `Text` `"[read error: <err>]"`, then stop that reader (the exit result still decides run status).
   - Each reader also observes a shared `readerStop` channel. A forced close signals that channel before closing the read ends; the resulting close error is treated as normal termination and must not become a synthetic read-error frame.
   - Use one shared `frameMu` for both index allocation **and the channel send**. `nextIndex` starts at 1. `emitFrame` assigns the next index and performs `select { case out <- frame: ...; case <-runCtx.Done(): return false; case <-readerStop: return false }` while holding that lock; a false result stops the reader. This makes the observed `out`/transcript order monotonic by `Frame.Index`, while still not claiming true child-stream interleaving.
7. Start the reader goroutines and then immediately start `waitErr := make(chan error, 1); go func(){ waitErr <- cmd.Wait() }()`; that goroutine is the sole caller of `cmd.Wait`. Because `cmd.Stdout` / `cmd.Stderr` are ordinary `*os.File` write ends, `Cmd.Wait` only reaps the process and does not own or wait for the reader goroutines. Declare `var processErr error` for the result below.
8. Select:
   - `case processErr = <-waitErr:` → the child has exited; wait for both readers to observe EOF or for `runCtx.Done()`. Use an exact **3 s output-drain grace**. If `runCtx.Done()` fires, invoke `stopReaders` immediately; if the readers do not finish (for example, a descendant retained a pipe), invoke the same helper after the grace. `stopReaders` signals `readerStop`, closes both read ends, then waits for the readers; a forced close must not emit a synthetic read-error frame. Invoke the idempotent helper on the normal EOF path too, so all descriptors are released before returning.
   - `case <-runCtx.Done():` → call `killProcessGroup(cmd)`, then wait for `waitErr`. To avoid a Go 1.19 pipe/descendant deadlock, use an exact **3 s termination grace**: if `waitErr` is not ready after 3 s, signal reader-stop, close both read ends, call `cmd.Process.Kill()` as a direct-child fallback, then receive `processErr = <-waitErr` and reap. The fallback is only after the process-group kill attempt. If `waitErr` becomes ready before the grace expires, assign that error to `processErr`; in either case, invoke `stopReaders` and wait for both readers before returning.
9. Status, in this order (runCtx wins over exit code):
   - `runCtx.Err() == context.DeadlineExceeded` → `StatusTimeout`
   - `runCtx.Err() == context.Canceled` → `StatusCancelled`
   - `processErr == nil` → `StatusCompleted` (`exit_code` 0)
   - `processErr` is `*exec.ExitError` → `StatusFailed`, `exit_code = ee.ExitCode()`
   - any other `processErr` → `StatusFailed`, `exit_code = null`
10. `ProviderMeta` = `{"provider": p.name, "exit_code": <int|null>}`. Do **not** persist `bin` or `args`: `AgentRun.ProviderMetadata` is returned by `agent.run.get/query`, so neither the full command nor environment values belong in `provider_metadata` or an extra RPC field. Optional stderr diagnostics may contain only a redacted provider name and error category, never the prompt, argv, environment, or credentials. The pre-existing `AgentRun.Prompt` field is not removed or redacted by M1.8.

### 4.3 Runtime discovery (`plugins/agent-harness/discovery.go`)

```go
func discoverProviders(candidates []string, envAllowlist []string, logw io.Writer) map[string]Provider
```

- Always seeds `{"mock": MockProvider{}}`.
- **`argvTemplates`** is a package map `map[string]func(RunSpec) []string` populated only with `"codex": codexArgv`. Discovery registers a candidate **only if it has a template** — an arbitrary executable name cannot be turned into a `RealProvider`. Tests may inject temporary entries and must restore the map in `defer`; these tests must not run in parallel.
- `candidates` default `["codex"]`, overridden by env `VIBE_AGENT_PROVIDERS` (comma-separated names).
- `candidatesFromEnv()` trims whitespace, drops empty entries, preserves first occurrence order, and uses `["codex"]` when the variable is unset or contains no non-empty entry.
- If `realProviderSupported() == false`, log one skipped-platform line and return the seeded `mock` map; never probe or register a real provider.
- For each candidate `name`:
  - `name == "mock"` → skip with a log line (`mock` is reserved; discovery never overwrites the seeded mock).
  - `argvTemplates[name]` absent → skip, log `no argv template for "<name>"`.
  - `exec.LookPath(name)` fails → skip, log `not on PATH`.
  - else run `<abs> --version` with a **2 s** timeout (`exec.Command` + `Start` + a timer) and the allowlisted env. Open `os.DevNull` once as a `*os.File`; on open failure, skip and log. Direct both `Stdout` and `Stderr` to that file, not `io.Discard`; this avoids `os/exec` copy goroutines and makes the probe's `Wait` bounded even if a child inherits descriptors. After a successful `Start`, the probe must always call `Wait`, close the dev-null file, stop its timer after the process exits, and on timeout call `killProcessGroup` plus the same 3 s direct-child reap fallback as §4.2. Exit 0 → `register RealProvider{name, bin: abs, argv: argvTemplates[name], env: allowlistedEnv(envAllowlist), timeout: 0}`. Non-zero / timeout / start error → skip, log the reason.
  - **Never fails plugin startup** — a failed probe is a skipped provider, nothing more.
  - **All discovery logging → `logw` (wired to `os.Stderr`).** Never stdout — that is the `vibe-plugin/1` protocol channel. On supported platforms, emit one line per candidate; on unsupported platforms, emit one skipped-platform line and do not probe.

**Env allowlist.** `allowlistedEnv(names)` returns a **non-nil** `[]string` containing only `NAME=value` for `NAME` in `names` present in `os.Environ()`, minus a hard, **non-overridable denylist**: any name matching `FAKE_AGENT_*` or starting `VIBE_` is dropped even if listed. Default `names`:

```
PATH HOME USER LOGNAME SHELL LANG LC_ALL LC_CTYPE TERM TMPDIR TZ
SSL_CERT_FILE SSL_CERT_DIR CODEX_HOME OPENAI_API_KEY CODEX_API_KEY
```

`VIBE_AGENT_ENV_ALLOWLIST` (comma-separated) **replaces** the default `names` list but cannot defeat the denylist. `RealProvider.env` is always this non-nil slice — the child never inherits the full parent environment.

`allowlistFromEnv()` trims whitespace, drops empty and duplicate names, and uses the default list when the override is unset or empty.

### 4.4 Handler changes (`plugins/agent-harness/handlers.go`, `main.go`)

- `runDeps`: `Prov Provider` → `Providers map[string]Provider` + `DefaultProvider string` + `Runs *runRegistry`. Update every existing `runDeps` fixture and direct `runOnce` call in `handlers_test.go` to provide the map/default/registry and the explicit provider argument; do not leave a second provider-selection path in tests.
- `agentRunHandler`:
  1. decode; validate `work_context_id / workspace_path / prompt`.
  2. `name := q.Provider; if name == "" { name = base.DefaultProvider; if name == "" { name = "mock" } }` — `mock` is the defensive default even for an incompletely constructed test dependency.
  3. `prov, ok := base.Providers[name]; if !ok { return INVALID "unknown provider \"<name>\"" }` — **before** any store write. (Deletes the old `"only mock provider is available in M1.3"` branch.)
  4. `runID := protocol.NewID("run")`; `ar.HarnessNativeID = name + "-" + runID`; `ar.Provider = name`; `RecordStarted(ar)`.
  5. `runCtx, runCancel := context.WithCancel(rc.Context())` — preserves M1.3's existing request/stream cancellation propagation; an external stream disconnect may therefore cancel the provider as the kernel already specifies. `runCancel` adds the **explicit** `agent.run.cancel` path without relying on the stream consumer. `done := make(chan struct{})`.
  6. `base.Runs.register(runID, runCancel, done)`; `go func() { defer func() { runCancel(); base.Runs.done(runID); close(done) }(); runOnce(runCtx, prov, d, ar, spec, out) }()` — **`prov` (the looked-up provider) is passed in**; `runOnce`'s signature grows a `prov Provider` param and it calls `runProvider(ctx, prov, …)` instead of `d.Prov`. `runDeps.Prov` is removed. The deferred cleanup makes `done` observable only after context cleanup and registry removal; it does not recover or hide a panic.
  7. return `{agent_run, stream_id}`.
- `main.go`: `Providers: discoverProviders(candidatesFromEnv(), allowlistFromEnv(), os.Stderr)`, `DefaultProvider: "mock"`, `Runs: newRunRegistry()`.

Extract the post-validation launch sequence into a helper that accepts an explicit `context.Context` and output channel (for example `startAgentRun(ctx, base, e, q, out)`). `agentRunHandler` supplies `rc.Context()` and then calls `rc.Stream(out)`; deterministic handler tests use `context.Background()` and drain `out` directly because `pluginhost.RequestContext` fields are intentionally private. This keeps the live-cancel test on the same provider-selection/registry path without constructing a fake `RequestContext`.

`runProvider` remains the existing transcript orchestrator, with one cancellation hardening: its per-frame mirror select must include `case <-ctx.Done():` alongside `case mirror <- f` and the existing 2 s bounded send. Once an explicit cancel is requested, a stalled stream consumer must not add up to 2 s per buffered frame before `runOnce` can persist the terminal record.

### 4.5 Live cancel (`plugins/agent-harness/runreg.go` + `cancelHandler`)

```go
type runEntry struct {
	cancel     context.CancelFunc
	cancelOnce sync.Once
	done       chan struct{}
}
type runRegistry struct {
	mu      sync.Mutex
	entries map[string]*runEntry
}
func newRunRegistry() *runRegistry
func (r *runRegistry) register(id string, c context.CancelFunc, done chan struct{})
func (r *runRegistry) stop(id string) (done <-chan struct{}, live bool) // keeps the entry until done(), calls cancel() once
func (r *runRegistry) done(id string)                                   // called by the run goroutine when finished; unregisters
```

**`cancelHandler(s *Store, runs *runRegistry)`** — a plain `pluginhost.Handler` (no `rc` needed); `main.go` wires this exact handler through `wrap`, and the stateful fallback write remains inside `fencing.WithWriteFence(e, ...)`:

1. decode `agent_run_id` (empty → `INVALID`).
2. `ar, ok := s.GetByID(id)` → not found → `NOT_FOUND`.
3. `if ar.Status != StatusRunning` → `CONFLICT "already <status>"` (idempotent guard; also covers a run that finished naturally moments earlier).
4. `done, live := runs.stop(id)` — `stop` finds the entry under the registry lock, leaves it registered until the run goroutine calls `done`, and invokes its `CancelFunc` exactly once via `sync.Once`. Concurrent cancel requests therefore all join the same live run and cannot fall through to the store-only fallback while the provider is still stopping. The store remains the authority for the terminal race.
5. **live**: the run goroutine will observe `runCtx` cancelled, the provider returns `StatusCancelled`, `runOnce` `blob.put`s the partial transcript and `Persist`s it via the existing `agent.run.completed` op (`RecordCompleted` requires `RUNNING` — still true; sole writer). Then:
   ```
   select {
   case <-done:                       // run goroutine finished + attempted persistence
       final, ok := s.GetByID(id)
       if !ok { return &protocol.Error{Code: "NOT_FOUND", Message: "agent run disappeared after cancel"} }
       if final.Status == StatusRunning { return &protocol.Error{Code: "IO", Retryable: true, Message: "cancel: provider stopped but terminal record was not persisted"} }
       return {agent_run: final}
   case <-time.After(30 * time.Second):
       return &protocol.Error{Code: "IO", Retryable: true, Message: "cancel: provider did not stop within 30s"}
   }
   ```
   **Race — natural completion vs. cancel.** If the provider returns any terminal status (`COMPLETED`, `FAILED`, or `TIMEOUT`) before it observes the cancel, `runOnce` persists that terminal state (still the sole writer). `cancelHandler` then reads the terminal state back and returns it — correct: the run finished before cancel took effect. `cancelHandler` never writes in the live path.
6. **not live** (`stop` reports the id absent — possible after a plugin restart, or because the run goroutine unregistered between steps 2 and 4): re-read the store. If it is now terminal, return `CONFLICT` using the existing already-terminal behavior. If it is still `RUNNING`, call `fencing.WithWriteFence(e, func() error { return s.RecordCancelled(id) })` (the existing `agent.run.cancelled` op). If that fenced write races with natural completion and returns `ErrAlreadyTerminal`, re-read and return `CONFLICT`; otherwise map errors as today. If the fallback write succeeds, return the `CANCELLED` `AgentRun`. **Known limitation (§11):** a child process left over from before a restart is not guaranteed dead — the process-group id was lost with the registry.

**No Store schema change.** Both ops (`agent.run.completed`, `agent.run.cancelled`) already exist; `RecordCompleted` already accepts an arbitrary status string; `runOnce` already persists `tr.Result.Status` (the mock already returns `CANCELLED` on ctx-done). Store locking ensures at most one terminal append succeeds while the record is `RUNNING`; all losing races map to the documented terminal/conflict behavior.

### 4.6 Provider passthrough

- **`RunSpec`**: no change (Provider selection is a handler concern, not a spec field).
- **`caps.AgentRun`** (`plugins/engineering-workflow/pipeline.go`): signature grows a leading `provider string` param: `AgentRun func(provider, wcID, wsPath, prompt, writeFile, writeContent string) (id, status string, err error)`.
- **`RunRequest`** (`pipeline.go`): `+Provider string` (empty → the pipeline passes `"mock"`).
- **`runPipeline`**: passes `req.Provider` (defaulted to `"mock"`) to `c.AgentRun`.
- **`realCaps`** (`plugins/engineering-workflow/handlers.go`): the `agent.run` command payload's `"provider"` becomes the passed value instead of the literal `"mock"`; `mock_write_file` / `mock_write_content` are still sent (ignored by RealProvider).
- Extract the payload construction into a pure helper such as `agentRunPayload(provider, wc, path, prompt, writeFile, writeContent)` and add a `handlers_test.go` assertion that a non-default provider survives into the `agent.run` payload; the pipeline fake-caps assertion alone cannot detect a hard-coded value inside `realCaps`.
- **`contracts/workflow.engineering.run/v1/schema.json`**: `+ "provider": {"type": "string"}` in request properties, **not** in `required` — an additive optional request field, compatible, contract count stays **31**.
- **`cli/vibe/main.go`**:
  - `commandWithDeadline(capability, payload, timeout)` creates the same command envelope as `command`, with `Deadline = now + timeout`; `command` remains the 30 s compatibility wrapper.
  - `agentRun`: `+ -provider` (default `"mock"`), `+ -timeout` (default `30s`; real codex startup+auth can exceed 30 s) — uses `commandWithDeadline` for the requested duration.
  - `agentCancel`: use a command deadline of **35 s or longer**; the exact 30 s provider-stop wait must not expire at the same time as the CLI request.
  - `workflowRun`: `+ -provider` (default `"mock"`), added to the payload only when non-empty.

### 4.7 `RunRequest` / workflow assertion

`plugins/engineering-workflow/pipeline_test.go`: a fake-caps test asserting `runPipeline` forwards `req.Provider` to `caps.AgentRun` verbatim, and that empty `req.Provider` forwards `"mock"`.

## 5. codex invocation (real provider #1)

`codex-cli` ≥ 0.151 is a reviewer/development-machine precondition, verified locally as `0.152.0`; discovery only requires a successful `--version` probe and does not parse or enforce a version number. Non-interactive form:

```
codex exec \
  --cd <workspace_path> \
  -s workspace-write \
  --approve-for-me \
  --skip-git-repo-check \
  --json \
  --color never \
  -- <prompt>
```

- `--cd` sets the working root (belt-and-braces with `cmd.Dir`).
- `-s workspace-write` + `--approve-for-me` = headless, may edit files in the workspace, auto-approves through the workspace-write sandbox. (Not `--dangerously-bypass-approvals-and-sandbox` — we keep codex's own sandbox on.)
- `--skip-git-repo-check` because the scratch workspace worktree may not look like a normal repo root to codex.
- `--json` emits JSONL events to stdout — **stored as opaque lines** (structured parsing is M1.8+/UI, §8 NON-GOAL). `--color never` keeps lines clean.
- Exit 0 on success, non-zero on failure.

Why codex over claude/gemini: all three are on the dev machine; codex `exec` has the most explicit headless contract (`-s` sandbox levels, `--approve-for-me`, `--json`, documented exit semantics) and a stable `--cd`. claude/gemini adapters are out of scope (§9).

## 6. Testing

### 6.1 Deterministic (dispatched)

**Fixture `plugins/agent-harness/fakeagentcli/main.go`** — a committed Go `main` package (in the `plugins` module; no `go.work`/`go.mod` change), built by `scripts/build.sh` to `.bin/fake-agent-cli`. Everything is argv (no env), so no test variable ever touches a production allowlist:

```
fake-agent-cli --version                      # prints "fake-agent-cli 0.0.1\n", exit 0
fake-agent-cli --version-exit N               # for discovery tests: print a version line, exit N
fake-agent-cli --cd DIR --write RELFILE --line TEXT --emit-bytes N --pid-file FILE --exit N --sleep MS -- PROMPT...
```

Behaviour of the run form: `chdir(--cd)`; if `--pid-file` is set, write `os.Getpid()` plus a newline to that path; print 3 short `stdout` lines + 1 `stderr` line derived from the prompt; if `--emit-bytes N` > 0, additionally print one `stdout` line of exactly N bytes (generated internally — never via argv); if `--write` set, append `--line` + `"\n"` to `<cd>/<relfile>`; sleep `--sleep` ms in short intervals; `os.Exit(--exit)`. The fixture need not handle SIGKILL; the test observes process-group termination.

**`real_provider_test.go`** — resolves the repository root from the test source location (the package test working directory is not the repository root), then points a `RealProvider` at the absolute `<repo>/.bin/fake-agent-cli` path with a test `argv` template and an explicit non-nil env slice. It asserts:

The test helper starts `Run` in a goroutine and drains `out` concurrently; it must not call `Run` synchronously and wait to read the channel afterward. The timeout/cancel cases pass an absolute `--pid-file` path, poll the recorded pid until it is gone, and fail within the test deadline if termination does not complete. Each helper registers cleanup that cancels the context, waits for `Run`, and kills any still-recorded test pid, so a failed test cannot leak a provider process into later tests.

| case | knobs | expected |
|---|---|---|
| completed + workspace change | `--write Calc.java --line "// hardened" --exit 0` | frames streamed, `Index` monotonic; `Status == COMPLETED`; file contains the line; `ProviderMeta.exit_code == 0` |
| mock knobs ignored | set `RunSpec.MockWriteFile` / `MockWriteContent` but omit fake CLI `--write` | `Status == COMPLETED`; no mock-selected file is created or modified |
| provider metadata redaction | any successful `RealProvider` run | decode `ProviderMeta`; exactly `provider` and `exit_code` keys; no `bin`, full `args`, env, or credential values |
| non-zero exit → FAILED | `--exit 7` | `Status == FAILED`; `ProviderMeta.exit_code == 7` |
| deadline → TIMEOUT | `RealProvider.timeout = 50ms`, `--sleep 5000` | `Status == TIMEOUT` within ~2 s; the child pid is gone |
| context cancel → CANCELLED | cancel ctx after first frame, `--sleep 5000` | `Status == CANCELLED`; `os.FindProcess`+signal-0 shows the pid gone |
| start failure → FAILED/null | `bin = "/nonexistent/xyz"` | `Status == FAILED`; `ProviderMeta.exit_code` is JSON `null` |
| malformed provider is fail-closed | `env == nil` (and separately `argv == nil`) | `Status == FAILED`; `ProviderMeta.exit_code` is JSON `null`; no subprocess is started |
| oversized line | `--emit-bytes 1572864` | one frame whose `Text` ends `…[truncated]` and is ≤ ~1 MiB; run still `COMPLETED` |

**`discovery_test.go`** — writes throwaway wrapper scripts into `t.TempDir()`, marks them executable, and prepends it to `PATH` (the fixture is for `real_provider_test`; discovery is tested with its own scripts). The subprocess/discovery test files carry `//go:build unix`; non-unix builds only compile the platform stub and retain the seeded `mock` provider.

| case | setup | expected |
|---|---|---|
| candidate registers | `argvTemplates` temporarily has test entries `"good"` and `"bad"`; `good` on PATH is `#!/bin/sh` `echo good 1.0; exit 0` | map has `mock` + `good`; `good` is a `RealProvider` with `bin` = the resolved abs path |
| `--version` non-zero | the temporary `"bad"` template points to a `bad` script that exits 1 | skipped; map has only `mock`; one stderr log line |
| `--version` timeout | a temporary `"slow"` candidate sleeps past 2 s | skipped; discovery returns within the bounded probe+reap window and leaves no slow probe process |
| not on PATH | candidate `ghost`, nothing on PATH | skipped; no error |
| no argv template | candidate `codex`-absent-template name | skipped, log `no argv template` |
| `mock` candidate | `VIBE_AGENT_PROVIDERS=mock` | skipped, log `reserved`; seeded `mock` intact |
| stdout untouched | any of the above, `logw` and stdout captured separately | nothing written to stdout; log lines only on `logw` |
| env denylist | `VIBE_AGENT_ENV_ALLOWLIST=FAKE_AGENT_X,PATH` with `FAKE_AGENT_X` set | `allowlistedEnv` result contains `PATH=…` but no `FAKE_AGENT_X` |

`handlers_test.go` (public validation plus the extracted handler launch path):

| case | expected |
|---|---|
| `provider: ""` | run uses `mock`; `ar.Provider == "mock"`; `ar.HarnessNativeID == "mock-"+runID` |
| `provider: "codex"` when registered (fake) | `ar.Provider == "codex"`; `ar.HarnessNativeID == "codex-"+runID` |
| `provider: "bogus"` | `INVALID`; **no `agent.run.started` record written** (assert `store` empty for that WC) |
| **live cancel** | start a long fake run through the handler launch helper with an absolute `--pid-file`; call `cancelHandler(runID)`; assert it blocks until the provider exits, the final record is `CANCELLED` with a non-empty `RawSessionRef`, and the recorded pid is gone |

`runreg_test.go` must cover the registry independently: repeated `stop` calls before `done` invoke the cancel function exactly once and return `live == true` with the matching `done` channel; `done` unregisters the run; a `stop` after `done` returns `live == false`; and a missing id never calls a cancel function. These tests use a channel or atomic counter rather than a real provider.

`session_test.go` adds a bounded-cancel case: use a provider that emits a frame and then exits when its context is cancelled, pass an unbuffered mirror with no consumer, cancel the context, and assert `runProvider` returns promptly instead of waiting for the existing 2 s per-frame mirror timeout.

### 6.2 Real (local only) — `scripts/verify-real-provider.sh`

The script starts with `set -euo pipefail`. Guard: `[ "${VIBE_REAL_PROVIDER:-}" = "codex" ] || { echo "SKIP: set VIBE_REAL_PROVIDER=codex to run"; exit 0; }`. **Not** sourced by `smoke.sh`, **not** in `check-arch.sh`, **not** a milestone-acceptance gate — it is a reviewer-run manual check whose output goes in the PR and §13.

Steps: first `cd "$(dirname "$0")/.."`, then `source "scripts/lib/kernel-harness.sh"` and `build_bins`; create the scratch repo under `$DATA/scratch-repo`, and make its initial commit with explicit inline identity (`git -C "$DATA/scratch-repo" -c user.email=test@example.com -c user.name=test -c commit.gpgsign=false commit ...`); rely on the harness EXIT trap for cleanup; `export VIBE_AGENT_PROVIDERS=codex` **before** `restart_kernel`; restart the kernel; as **`m1-dev`** — create a task + `workspace.allocate` a real worktree over that repo; invoke `".bin/vibe" -socket "$SOCK" -identity "m1-dev" -token "$DEV_TOKEN" agent run <wc> -workspace <path> -provider codex -timeout 5m -prompt "Add a one-line Javadoc above the add method in Calc.java."`; capture the accepted run id and then assert:

- frames streamed to the CLI (`»` lines present);
- `agent.run.query {work_context_id}` → exactly one run with the captured id and `provider == "codex"` (guards against silently running mock);
- `agent.run.get` → `status == "COMPLETED"`;
- `git -C <worktree> diff --stat` is **non-empty** (codex actually changed the file);
- the `raw_session_ref` blob (`blob.get`) is non-empty and contains the assembled transcript.

Use `".bin/vibe-raw"` with the explicit `default-agent-harness/agent-runs-main` and `default-blob/blob-main` bindings for the query/get/blob assertions; save each raw response before parsing it so a failed assertion retains diagnostics. Redact any credential-bearing transcript lines before copying output into a PR or document.

Print `REAL PROVIDER (codex) VERIFY: OK`.

### 6.3 致残对照 (falsification)

| mutation | red |
|---|---|
| in `RealProvider.Run` step 9, drop the `*exec.ExitError` branch (always `COMPLETED`) | `real_provider_test.go` "non-zero exit → FAILED" |
| drop the `runCtx.Err()` checks in step 9 (map on exit `err` only) | "deadline → TIMEOUT" and "context cancel → CANCELLED" |
| map `RunSpec.Mock*` fields into `codexArgv` | `real_provider_test.go` "mock knobs ignored" (unexpected file change) |
| in `real_provider_exec_unix.go`, make `killProcessGroup` a no-op | timeout/cancel tests exceed their bounded timing before the 3 s direct-child fallback |
| in `cancelHandler`, drop the `<-done` wait (return right after `stop`) | `handlers_test.go` "live cancel" (final record still `RUNNING`, or `RawSessionRef` empty) |
| in `agentRunHandler`, move the unknown-provider check after `RecordStarted` | `handlers_test.go` "bogus" (an orphan `RUNNING` record appears) |
| in `discoverProviders`, let a `mock` candidate through | `discovery_test.go` "mock candidate" (seeded mock overwritten) |
| in `allowlistedEnv`, drop the denylist | `discovery_test.go` "env denylist" (`FAKE_AGENT_X` leaks) |
| add `bin` or `args` back into `ProviderMeta` | `real_provider_test.go` "provider metadata redaction" |
| revert `realCaps` to the literal `"provider": "mock"` | `engineering-workflow/handlers_test.go` payload-forwarding assertion |

## 7. Acceptance

**Dispatched** (implementer runs, raw output in PR):

```
1. three-module build (+ .bin/fake-agent-cli)     → exit 0
2. go test plugins + _template + cli + kernel     → all ok (incl. real_provider / discovery / handlers / pipeline provider tests)
2a. go test -race agent-harness                   → no race reports
3. kernel m05 regression                          → PASSED
4. check-arch                                     → 31 contracts / 10 manifests, ARCH CHECKS OK
5. DONE-integrity qualification ×3                → OK (provider work must not regress it)
6. smoke ×5                                       → M1 SMOKE: PASSED, 0 FAIL, 0 orphans (mock still default)
7. 致残对照 sweep (§6.3)                           → each red, reverted green
8. G1: git diff --name-only "$BASE" HEAD -- "kernel" → empty; full diff == the file list in §8
```

**Local** (reviewer, after merge or on the branch): `VIBE_REAL_PROVIDER=codex bash "scripts/verify-real-provider.sh"` → `REAL PROVIDER (codex) VERIFY: OK`; transcript pasted into the §13 bump commit / PR thread.

## 8. Files

New:
- `plugins/agent-harness/real_provider.go` + `real_provider_test.go`
- `plugins/agent-harness/real_provider_exec_unix.go` (`//go:build unix`) + `real_provider_exec_other.go` (`//go:build !unix`)
- `plugins/agent-harness/discovery.go` + `discovery_test.go`
- `plugins/agent-harness/runreg.go` + `runreg_test.go`
- `plugins/agent-harness/fakeagentcli/main.go` (test fixture, `package main`, stdlib only)
- `scripts/verify-real-provider.sh` (mode `0755`; credential-gated and safe to skip)

Modified:
- `plugins/agent-harness/handlers.go` (Providers map, DefaultProvider, unknown-before-RecordStarted, decoupled runCtx, run registry wiring, live cancel)
- `plugins/agent-harness/handlers_test.go` (public-path cases)
- `plugins/agent-harness/provider_test.go` (synchronize the existing result assertion for race-clean tests)
- `plugins/agent-harness/session.go` (stop mirroring promptly when the provider context is cancelled)
- `plugins/agent-harness/session_test.go` (bounded mirror-cancel regression)
- `plugins/agent-harness/main.go` (discovery wiring; `cancelHandler(s, runs)` wiring instead of the old store-only handler)
- `plugins/engineering-workflow/pipeline.go` (`RunRequest.Provider`, `caps.AgentRun` signature, forward in `runPipeline`)
- `plugins/engineering-workflow/handlers.go` (`realCaps` forwards the provider)
- `plugins/engineering-workflow/handlers_test.go` (realCaps payload provider assertion)
- `plugins/engineering-workflow/pipeline_test.go` (+ provider-forwarding test; existing fakes updated for the new `caps.AgentRun` signature)
- `cli/vibe/main.go` (`commandWithDeadline`; `agentRun` `-provider` + `-timeout`; `agentCancel` deadline; `workflowRun` `-provider`)
- `contracts/workflow.engineering.run/v1/schema.json` (+ optional `provider`)
- `scripts/build.sh` (`go build -o ".bin/fake-agent-cli" "./plugins/agent-harness/fakeagentcli"`)

Untouched during dispatched implementation: `kernel/`, `docs/M1-DESIGN.md` (the reviewer's post-merge reconciliation must update §6/§8's `harness_native_id` wording and §13's M1.8 codex-only status), `config/m1-{policy,bindings}.json` (agent.run is already granted; the real provider is plugin-internal), `plugins/manifests/*` (agent-harness manifest already has `permissions: ["exec:agent","fs:write"]`), `scripts/{smoke,qualify-done-integrity,check-arch}.sh`.

## 9. NON-GOALS (M1.8)

- **Structured transcript parsing** — codex `--json` events → typed cards / business fields. M1.8+ / UI phase. M1.8 stores opaque lines only.
- **Automatic provider fallback** and **provider-specific business adapters**. Multiple discovered candidates *may* be registered and *explicitly* selected; there is no auto-selection beyond "default = mock", and no per-provider special-casing in handlers.
- **claude / gemini adapters.** codex only. (The discovery mechanism is generic; only codex's `argv` template ships.)
- **Real provider through the full §10 chain + kill-runtime + restart-kernel + recovery.** That is M1.9.
- **`harness_native_id` = codex's real thread id.** M1.8 ships the synthetic `codex-<run_id>`; the §6/§8 "provider 私有会话标识" meaning is deferred to structured-transcript work.
- Changing the `agent.run@1` / stream contract shape.

## 10. Dispatch

Same protocol as M1.5–M1.7:
- clean tarball via `git clone --no-hardlinks` + orphan squash + strip tags, **no `tar --exclude` basename globs**;
- plan + dispatch prompt: check-self-fix-escalate preconditions, degrade-and-log fallbacks, budget-exhaustion protocol, stop criteria = load-bearing signals (kernel change / M0.5 red / a design invariant §2 can't hold / contract shape change / new external Go dep / 致残 not going red);
- `BASE=$(git rev-parse HEAD)` before Task 1; no hardcoded SHA;
- **the dispatched agent does NOT run codex** (no network) — it builds and tests against `.bin/fake-agent-cli` only; the prompt says so explicitly and tells it the real check is the reviewer's;
- reviewer fetches, re-runs §7 dispatched acceptance + the 致残 sweep independently, runs §6.2 locally, merges, tags `m1.8-real-provider-adapter`, then reconciles `docs/M1-DESIGN.md` §6/§8/§13 with the synthetic-id and codex-only decisions.

## 11. Known limitations (M1.8, documented not fixed)

- **Cancel after a plugin restart.** If `org.vibe.agent.harness` restarts while a real run is in flight, the run registry (and the child's process-group id) is lost. A later `agent.run.cancel` marks the record `CANCELLED` (via `agent.run.cancelled`) but cannot guarantee the orphaned child/process-group is dead and cannot reconstruct the partial transcript. Full restart+recovery of in-flight real runs is M1.9.
- **Transcript fidelity.** `raw_session_ref` is the adapter's line-assembled transcript with a single monotonic `Index`, not a byte-exact or truly-interleaved copy of the child's stdout/stderr.
- **Aggregate transcript size.** M1.8 caps each logical line at 1 MiB but retains the existing M1.3 whole-transcript accumulation behavior; it does not add an aggregate byte/frame quota. Resource/quota hardening is a later milestone.
- **Clean-exit descendants.** If a provider exits while a descendant retains its stdout/stderr descriptor, M1.8 bounds the output-drain wait and returns after closing the reader ends, but does not promise to kill that descendant. Cancellation/timeout uses process-group termination; full descendant lifecycle supervision is deferred.
- **`harness_native_id`** is the synthetic `codex-<run_id>`; codex's own session/thread id is not extracted (no `--json` parsing).
- **Non-unix.** `RealProvider` process-group semantics are POSIX-only; on non-unix builds `discoverProviders` registers no real providers.
