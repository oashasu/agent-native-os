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

## 3. RPC boundary

| Field | Origin | Ownership |
|---|---|---|
| `agent.run` request `.provider` | caller, optional | provider selection hint |
| `workflow.engineering.run` request `.provider` | caller, optional | passed straight through to `agent.run` |
| default provider | server | `mock` |
| executable path, argv, env, credentials | server-internal (`discovery.go` / `RealProvider`) | never on the wire |
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

Process spawn + group kill live in platform files so a non-unix build still compiles:
- `real_provider_exec_unix.go` (`//go:build unix`): `startProcess(cmd)` sets `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}` then `cmd.Start()`; `killProcessGroup(cmd)` = `syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)`.
- `real_provider_exec_other.go` (`//go:build !unix`): `startProcess` returns an error `"real provider unsupported on this platform"`; `killProcessGroup` = `cmd.Process.Kill()`. (`discoverProviders` therefore registers no real providers on non-unix.)

`Run(ctx, spec, out)`:

1. `defer close(out)`.
2. `args := p.argv(spec)` (§5 for codex).
3. If `p.timeout > 0`, wrap: `ctx2, cancel := context.WithTimeout(ctx, p.timeout); defer cancel()` and use `ctx2` below (don't shadow with `:=` inside an `if`).
4. `cmd := exec.Command(p.bin, args...)`; `cmd.Dir = spec.WorkspacePath`; `cmd.Env = p.env`.
5. `stdout, _ := cmd.StdoutPipe()`; `stderr, _ := cmd.StderrPipe()`. `if err := startProcess(cmd); err != nil` → `RunResult{Status: StatusFailed, ProviderMeta: meta(p, args, nil)}` (`exit_code` JSON `null`).
6. Two reader goroutines (one per pipe). Each reads with `bufio.Reader.ReadString('\n')`:
   - Trim a trailing `\n` and one optional preceding `\r` from the line before use.
   - Accumulate up to **1 MiB**; on a longer line, emit one frame whose `Text` is the first 1 MiB followed by `"…[truncated]"`, then discard bytes until the next `\n` and continue.
   - On `io.EOF` with non-empty residual (no trailing newline) → emit that residual as a final frame.
   - On a non-EOF read error → emit one `stderr` frame `"[read error: <err>]"` and stop that reader (do not fail the run here; the exit code decides).
   - Each line → `out <- Frame{Kind: "stdout"|"stderr", Text: <line>, Index: nextIndex()}`; `nextIndex()` is one mutex-guarded counter shared by both goroutines. **Only `Frame.Index` monotonicity is guaranteed — not the true stdout/stderr interleaving.**
7. `waitErr := make(chan error, 1); go func(){ readersWG.Wait(); waitErr <- cmd.Wait() }()`.
8. Select:
   - `case err := <-waitErr:` → map (step 9).
   - `case <-ctx.Done():` → `killProcessGroup(cmd)`; `<-waitErr` (reap); status from `ctx.Err()`.
9. Status, in this order (ctx wins over exit code):
   - `ctx.Err() == context.DeadlineExceeded` → `StatusTimeout`
   - `ctx.Err() == context.Canceled` → `StatusCancelled`
   - `err == nil` → `StatusCompleted` (`exit_code` 0)
   - `err` is `*exec.ExitError` → `StatusFailed`, `exit_code = ee.ExitCode()`
   - any other `err` → `StatusFailed`, `exit_code = null`
10. `ProviderMeta` = `{"provider": p.name, "bin": p.bin, "args": args, "exit_code": <int|null>}` (the binary is `bin`, not `args[0]`).

### 4.3 Runtime discovery (`plugins/agent-harness/discovery.go`)

```go
func discoverProviders(candidates []string, envAllowlist []string, logw io.Writer) map[string]Provider
```

- Always seeds `{"mock": MockProvider{}}`.
- **`argvTemplates`** is a package map `map[string]func(RunSpec) []string` populated only with `"codex": codexArgv`. Discovery registers a candidate **only if it has a template** — an arbitrary executable name cannot be turned into a `RealProvider`.
- `candidates` default `["codex"]`, overridden by env `VIBE_AGENT_PROVIDERS` (comma-separated names).
- For each candidate `name`:
  - `name == "mock"` → skip with a log line (`mock` is reserved; discovery never overwrites the seeded mock).
  - `argvTemplates[name]` absent → skip, log `no argv template for "<name>"`.
  - `exec.LookPath(name)` fails → skip, log `not on PATH`.
  - else run `<abs> --version` with a **2 s** timeout (`exec.Command` + `Start` + a timer that `killProcessGroup`s) and the allowlisted env. Exit 0 → `register RealProvider{name, bin: abs, argv: argvTemplates[name], env: allowlistedEnv(envAllowlist), timeout: 0}`. Non-zero / timeout / start error → skip, log the reason.
  - **Never fails plugin startup** — a failed probe is a skipped provider, nothing more.
- **All discovery logging → `logw` (wired to `os.Stderr`).** Never stdout — that is the `vibe-plugin/1` protocol channel. One line per candidate.

**Env allowlist.** `allowlistedEnv(names)` returns a **non-nil** `[]string` containing only `NAME=value` for `NAME` in `names` present in `os.Environ()`, minus a hard, **non-overridable denylist**: any name matching `FAKE_AGENT_*` or starting `VIBE_` is dropped even if listed. Default `names`:

```
PATH HOME USER LOGNAME SHELL LANG LC_ALL LC_CTYPE TERM TMPDIR TZ
SSL_CERT_FILE SSL_CERT_DIR CODEX_HOME OPENAI_API_KEY CODEX_API_KEY
```

`VIBE_AGENT_ENV_ALLOWLIST` (comma-separated) **replaces** the default `names` list but cannot defeat the denylist. `RealProvider.env` is always this non-nil slice — the child never inherits the full parent environment.

### 4.4 Handler changes (`plugins/agent-harness/handlers.go`, `main.go`)

- `runDeps`: `Prov Provider` → `Providers map[string]Provider` + `DefaultProvider string` + `Runs *runRegistry`.
- `agentRunHandler`:
  1. decode; validate `work_context_id / workspace_path / prompt`.
  2. `name := q.Provider; if name == "" { name = base.DefaultProvider }`.
  3. `prov, ok := base.Providers[name]; if !ok { return INVALID "unknown provider \"<name>\"" }` — **before** any store write. (Deletes the old `"only mock provider is available in M1.3"` branch.)
  4. `runID := protocol.NewID("run")`; `ar.HarnessNativeID = name + "-" + runID`; `ar.Provider = name`; `RecordStarted(ar)`.
  5. `runCtx, runCancel := context.WithCancel(rc.Context())` — same lifetime as today's `go runOnce(rc.Context(), …)` (M1.3 already settled "client disconnect ≠ business cancel" under this context, and it passed review); `runCancel` adds the **explicit** cancel path. `done := make(chan struct{})`.
  6. `base.Runs.register(runID, runCancel, done)`; `go func() { runOnce(runCtx, prov, d, ar, spec, out); base.Runs.done(runID); close(done) }()` — **`prov` (the looked-up provider) is passed in**; `runOnce`'s signature grows a `prov Provider` param and it calls `runProvider(ctx, prov, …)` instead of `d.Prov`. `runDeps.Prov` is removed.
  7. return `{agent_run, stream_id}`.
- `main.go`: `Providers: discoverProviders(candidatesFromEnv(), allowlistFromEnv(), os.Stderr)`, `DefaultProvider: "mock"`, `Runs: newRunRegistry()`.

### 4.5 Live cancel (`plugins/agent-harness/runreg.go` + `cancelHandler`)

```go
type runRegistry struct {
	mu     sync.Mutex
	cancel map[string]context.CancelFunc
	done   map[string]chan struct{}
}
func newRunRegistry() *runRegistry
func (r *runRegistry) register(id string, c context.CancelFunc, done chan struct{})
func (r *runRegistry) stop(id string) (done <-chan struct{}, live bool) // calls cancel() (idempotent), returns the done chan
func (r *runRegistry) done(id string)                                   // called by the run goroutine when finished; unregisters
```

**`cancelHandler(s *Store, runs *runRegistry)`** — a plain `pluginhost.Handler` (no `rc` needed):

1. decode `agent_run_id` (empty → `INVALID`).
2. `ar, ok := s.GetByID(id)` → not found → `NOT_FOUND`.
3. `if ar.Status != StatusRunning` → `CONFLICT "already <status>"` (idempotent guard; also covers a run that finished naturally moments earlier).
4. `done, live := runs.stop(id)` — `stop` calls the registered `CancelFunc` (calling an already-called one is a no-op) and returns the run's `done` channel.
5. **live**: the run goroutine will observe `runCtx` cancelled, the provider returns `StatusCancelled`, `runOnce` `blob.put`s the partial transcript and `Persist`s it via the existing `agent.run.completed` op (`RecordCompleted` requires `RUNNING` — still true; sole writer). Then:
   ```
   select {
   case <-done:                       // run goroutine finished + persisted
       final, _ := s.GetByID(id); return {agent_run: final}
   case <-time.After(30 * time.Second):
       return &protocol.Error{Code: "IO", Retryable: true, Message: "cancel: provider did not stop within 30s"}
   }
   ```
   **Race — natural completion vs. cancel.** If the provider returns `COMPLETED`/`FAILED` before it observes the cancel, `runOnce` persists that terminal state (still the sole writer). `cancelHandler` then reads `COMPLETED`/`FAILED` back and returns it — correct: the run finished before cancel took effect. `cancelHandler` never writes in the live path.
6. **not live** (`stop` reports the id absent — only reachable after a plugin restart lost the registry while the store still shows `RUNNING`): `s.RecordCancelled(id)` (the existing `agent.run.cancelled` op; requires `RUNNING` — true; sole writer here), return the `AgentRun`. **Known limitation (§11):** a child process left over from before the restart is not guaranteed dead — the process-group id was lost with the registry.

**No Store schema change.** Both ops (`agent.run.completed`, `agent.run.cancelled`) already exist; `RecordCompleted` already accepts an arbitrary status string; `runOnce` already persists `tr.Result.Status` (the mock already returns `CANCELLED` on ctx-done). In every path exactly one writer touches the record while it is `RUNNING`.

### 4.6 Provider passthrough

- **`RunSpec`**: no change (Provider selection is a handler concern, not a spec field).
- **`caps.AgentRun`** (`plugins/engineering-workflow/pipeline.go`): signature grows a leading `provider string` param: `AgentRun func(provider, wcID, wsPath, prompt, writeFile, writeContent string) (id, status string, err error)`.
- **`RunRequest`** (`pipeline.go`): `+Provider string` (empty → the pipeline passes `"mock"`).
- **`runPipeline`**: passes `req.Provider` (defaulted to `"mock"`) to `c.AgentRun`.
- **`realCaps`** (`plugins/engineering-workflow/handlers.go`): the `agent.run` command payload's `"provider"` becomes the passed value instead of the literal `"mock"`; `mock_write_file` / `mock_write_content` are still sent (ignored by RealProvider).
- **`contracts/workflow.engineering.run/v1/schema.json`**: `+ "provider": {"type": "string"}` in request properties, **not** in `required` — an additive optional request field, compatible, contract count stays **31**.
- **`cli/vibe/main.go`**:
  - `agentRun`: `+ -provider` (default `"mock"`), `+ -timeout` (default `30s`; real codex startup+auth can exceed 30 s) — replaces the fixed `command("agent.run", …)` 30 s deadline with `commandWithDeadline`.
  - `workflowRun`: `+ -provider` (default `"mock"`), added to the payload only when non-empty.

### 4.7 `RunRequest` / workflow assertion

`plugins/engineering-workflow/pipeline_test.go`: a fake-caps test asserting `runPipeline` forwards `req.Provider` to `caps.AgentRun` verbatim, and that empty `req.Provider` forwards `"mock"`.

## 5. codex invocation (real provider #1)

`codex-cli` ≥ 0.151, verified on the dev machine. Non-interactive form:

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
fake-agent-cli --cd DIR --write RELFILE --line TEXT --emit-bytes N --exit N --sleep MS -- PROMPT...
```

Behaviour of the run form: `chdir(--cd)`; print 3 short `stdout` lines + 1 `stderr` line derived from the prompt; if `--emit-bytes N` > 0, additionally print one `stdout` line of exactly N bytes (generated internally — never via argv); if `--write` set, append `--line` + `"\n"` to `<cd>/<relfile>`; sleep `--sleep` ms in an interruptible loop that exits fast on SIGTERM/SIGINT/SIGKILL; `os.Exit(--exit)`.

**`real_provider_test.go`** — builds a `RealProvider` pointed at an absolute path to `.bin/fake-agent-cli` (resolved once via `filepath.Abs`) with a test `argv` template, and asserts:

| case | knobs | expected |
|---|---|---|
| completed + workspace change | `--write Calc.java --line "// hardened" --exit 0` | frames streamed, `Index` monotonic; `Status == COMPLETED`; file contains the line; `ProviderMeta.exit_code == 0` |
| non-zero exit → FAILED | `--exit 7` | `Status == FAILED`; `ProviderMeta.exit_code == 7` |
| deadline → TIMEOUT | `RealProvider.timeout = 50ms`, `--sleep 5000` | `Status == TIMEOUT` within ~2 s |
| context cancel → CANCELLED | cancel ctx after first frame, `--sleep 5000` | `Status == CANCELLED`; `os.FindProcess`+signal-0 shows the pid gone |
| start failure → FAILED/null | `bin = "/nonexistent/xyz"` | `Status == FAILED`; `ProviderMeta.exit_code` is JSON `null` |
| oversized line | `--emit-bytes 1572864` | one frame whose `Text` ends `…[truncated]` and is ≤ ~1 MiB; run still `COMPLETED` |

**`discovery_test.go`** — writes throwaway wrapper scripts into `t.TempDir()` and prepends it to `PATH` (the fixture is for `real_provider_test`; discovery is tested with its own scripts):

| case | setup | expected |
|---|---|---|
| candidate registers | `argvTemplates` temporarily has a test entry `"good"`; `good` on PATH is `#!/bin/sh` `echo good 1.0; exit 0` | map has `mock` + `good`; `good` is a `RealProvider` with `bin` = the resolved abs path |
| `--version` non-zero | `bad` script exits 1 | skipped; map has only `mock`; one stderr log line |
| not on PATH | candidate `ghost`, nothing on PATH | skipped; no error |
| no argv template | candidate `codex`-absent-template name | skipped, log `no argv template` |
| `mock` candidate | `VIBE_AGENT_PROVIDERS=mock` | skipped, log `reserved`; seeded `mock` intact |
| stdout untouched | any of the above, `logw` and stdout captured separately | nothing written to stdout; log lines only on `logw` |
| env denylist | `VIBE_AGENT_ENV_ALLOWLIST=FAKE_AGENT_X,PATH` with `FAKE_AGENT_X` set | `allowlistedEnv` result contains `PATH=…` but no `FAKE_AGENT_X` |

`handlers_test.go` (public path):

| case | expected |
|---|---|
| `provider: ""` | run uses `mock`; `ar.Provider == "mock"`; `ar.HarnessNativeID == "mock-"+runID` |
| `provider: "codex"` when registered (fake) | `ar.Provider == "codex"`; `ar.HarnessNativeID == "codex-"+runID` |
| `provider: "bogus"` | `INVALID`; **no `agent.run.started` record written** (assert `store` empty for that WC) |
| **live cancel** | start a long fake run through `agentRunHandler`; call `cancelHandler(runID)`; assert it blocks until the provider exits, the final record is `CANCELLED` with a non-empty `RawSessionRef`, and the process is gone |

### 6.2 Real (local only) — `scripts/verify-real-provider.sh`

Guard: `[ "${VIBE_REAL_PROVIDER:-}" = "codex" ] || { echo "SKIP: set VIBE_REAL_PROVIDER=codex to run"; exit 0; }`. **Not** sourced by `smoke.sh`, **not** in `check-arch.sh`, **not** a milestone-acceptance gate — it is a reviewer-run manual check whose output goes in the PR and §13.

Steps: source `scripts/lib/kernel-harness.sh`; `VIBE_AGENT_PROVIDERS=codex` in the kernel env; `restart_kernel`; as **`m1-dev`** — create a task + `workspace.allocate` a real worktree over a scratch git repo containing `Calc.java`; `vibe agent run <wc> -workspace <path> -provider codex -timeout 5m -prompt "Add a one-line Javadoc above the add method in Calc.java."`; then assert:

- frames streamed to the CLI (`»` lines present);
- `agent.run.query {work_context_id}` → the run's `provider == "codex"` (guards against silently running mock);
- `agent.run.get` → `status == "COMPLETED"`;
- `git -C <worktree> diff --stat` is **non-empty** (codex actually changed the file);
- the `raw_session_ref` blob (`blob.get`) is non-empty and contains the assembled transcript.

Print `REAL PROVIDER (codex) VERIFY: OK`.

### 6.3 致残对照 (falsification)

| mutation | red |
|---|---|
| in `RealProvider.Run` step 9, drop the `*exec.ExitError` branch (always `COMPLETED`) | `real_provider_test.go` "non-zero exit → FAILED" |
| drop the `ctx.Err()` checks in step 9 (map on exit `err` only) | "deadline → TIMEOUT" and "context cancel → CANCELLED" |
| in `real_provider_exec_unix.go`, make `killProcessGroup` a no-op | "context cancel → CANCELLED" (pid still alive) / "deadline → TIMEOUT" hangs |
| in `cancelHandler`, drop the `<-done` wait (return right after `stop`) | `handlers_test.go` "live cancel" (final record still `RUNNING`, or `RawSessionRef` empty) |
| in `agentRunHandler`, move the unknown-provider check after `RecordStarted` | `handlers_test.go` "bogus" (an orphan `RUNNING` record appears) |
| in `discoverProviders`, let a `mock` candidate through | `discovery_test.go` "mock candidate" (seeded mock overwritten) |
| in `allowlistedEnv`, drop the denylist | `discovery_test.go` "env denylist" (`FAKE_AGENT_X` leaks) |
| revert `realCaps` to the literal `"provider": "mock"` | `pipeline_test.go` provider-forwarding assertion |

## 7. Acceptance

**Dispatched** (implementer runs, raw output in PR):

```
1. three-module build (+ .bin/fake-agent-cli)     → exit 0
2. go test plugins + _template + cli + kernel     → all ok (incl. real_provider / discovery / handlers / pipeline provider tests)
3. kernel m05 regression                          → PASSED
4. check-arch                                     → 31 contracts / 10 manifests, ARCH CHECKS OK
5. DONE-integrity qualification ×3                → OK (provider work must not regress it)
6. smoke ×5                                       → M1 SMOKE: PASSED, 0 FAIL, 0 orphans (mock still default)
7. 致残对照 sweep (§6.3)                           → each red, reverted green
8. G1: git diff --name-only "$BASE" HEAD -- kernel → empty; full diff == the file list in §8
```

**Local** (reviewer, after merge or on the branch): `VIBE_REAL_PROVIDER=codex bash scripts/verify-real-provider.sh` → `REAL PROVIDER (codex) VERIFY: OK`; transcript pasted into the §13 bump commit / PR thread.

## 8. Files

New:
- `plugins/agent-harness/real_provider.go` + `real_provider_test.go`
- `plugins/agent-harness/real_provider_exec_unix.go` (`//go:build unix`) + `real_provider_exec_other.go` (`//go:build !unix`)
- `plugins/agent-harness/discovery.go` + `discovery_test.go`
- `plugins/agent-harness/runreg.go` + `runreg_test.go`
- `plugins/agent-harness/fakeagentcli/main.go` (test fixture, `package main`, stdlib only)
- `scripts/verify-real-provider.sh`

Modified:
- `plugins/agent-harness/handlers.go` (Providers map, DefaultProvider, unknown-before-RecordStarted, decoupled runCtx, run registry wiring, live cancel)
- `plugins/agent-harness/handlers_test.go` (public-path cases)
- `plugins/agent-harness/main.go` (discovery wiring)
- `plugins/engineering-workflow/pipeline.go` (`RunRequest.Provider`, `caps.AgentRun` signature, forward in `runPipeline`)
- `plugins/engineering-workflow/handlers.go` (`realCaps` forwards the provider)
- `plugins/engineering-workflow/pipeline_test.go` (+ provider-forwarding test; existing fakes updated for the new `caps.AgentRun` signature)
- `cli/vibe/main.go` (`agentRun` `-provider` + `-timeout`; `workflowRun` `-provider`)
- `contracts/workflow.engineering.run/v1/schema.json` (+ optional `provider`)
- `scripts/build.sh` (`go build -o .bin/fake-agent-cli ./plugins/agent-harness/fakeagentcli`)

Untouched: `kernel/`, `docs/M1-DESIGN.md` (§13 is the reviewer's post-merge bump), `config/m1-{policy,bindings}.json` (agent.run is already granted; the real provider is plugin-internal), `plugins/manifests/*` (agent-harness manifest already has `permissions: ["exec:agent","fs:write"]`), `scripts/{smoke,qualify-done-integrity,check-arch}.sh`.

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
- reviewer fetches, re-runs §7 dispatched acceptance + the 致残 sweep independently, runs §6.2 locally, merges, tags `m1.8-real-provider-adapter`, bumps §13.

## 11. Known limitations (M1.8, documented not fixed)

- **Cancel after a plugin restart.** If `org.vibe.agent.harness` restarts while a real run is in flight, the run registry (and the child's process-group id) is lost. A later `agent.run.cancel` marks the record `CANCELLED` (via `agent.run.cancelled`) but cannot guarantee the orphaned child/process-group is dead. Full restart+recovery of in-flight real runs is M1.9.
- **Transcript fidelity.** `raw_session_ref` is the adapter's line-assembled transcript with a single monotonic `Index`, not a byte-exact or truly-interleaved copy of the child's stdout/stderr.
- **`harness_native_id`** is the synthetic `codex-<run_id>`; codex's own session/thread id is not extracted (no `--json` parsing).
- **Non-unix.** `RealProvider` process-group semantics are POSIX-only; on non-unix builds `discoverProviders` registers no real providers.
