# M1.8 — Agent Adapter: Real Provider #1 (codex) — Design

**Status:** for review (2026-09-01)
**Spec source:** `docs/M1-DESIGN.md` §8 (真实 Harness Adapter — provider-neutral, runtime discovery, dual-track), §6 (`AgentRun` data model), §13 (milestone M1.8). Also `docs/superpowers/plans/2026-08-29-m1-3-agent-adapter.md` (which deferred "actually interrupt the in-flight goroutine" to M1.8).
**Milestone position:** after M1.7 (`m1.7-done-integrity-qualification`, main `621871c`), before M1.9 (full §10 qualification with the real provider).

---

## 1. Goal

Give `org.vibe.agent.harness` a **`RealProvider`** — a subprocess-driven implementation of the existing `Provider` interface — with **`codex`** as real provider #1, discovered at plugin startup and selectable per run. The `mock` provider stays the default everywhere, so `smoke.sh` and `qualify-done-integrity.sh` remain deterministic. `agent.run.cancel` is upgraded from a store-only status flip to a real interruption of the running provider (deferred here from M1.3).

The real end-to-end path (a real `codex` making a real change) is verified **only locally** by a credential-gated manual script — the air-gapped dispatch sandbox cannot run it. The dispatched work is the deterministic plumbing, exercised through a committed fake-CLI fixture.

## 2. Design invariants (must hold at merge)

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

### 4.2 `RealProvider` (`plugins/agent-harness/real_provider.go`)

```go
type RealProvider struct {
	name    string                    // "codex"
	bin     string                    // absolute path from exec.LookPath
	argv    func(spec RunSpec) []string
	env     []string                  // pre-computed allowlisted environment
	timeout time.Duration             // 0 = no adapter-imposed deadline (caller's ctx still applies)
}
```

`Run(ctx, spec, out)`:

1. `defer close(out)`.
2. Build `args := p.argv(spec)`. For codex (see §5): `codex exec --cd <ws> -s workspace-write --approve-for-me --skip-git-repo-check --json --color never -- <prompt>`.
3. If `p.timeout > 0`: `ctx, cancel = context.WithTimeout(ctx, p.timeout); defer cancel()`.
4. `cmd := exec.CommandContext(ctx, p.bin, args...)`; `cmd.Dir = spec.WorkspacePath`; `cmd.Env = p.env`; `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}`.
5. `cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }` (kill the **process group**, not just the direct child); `cmd.WaitDelay = 3 * time.Second`.
6. Wire `cmd.StdoutPipe()` / `cmd.StderrPipe()`. If `cmd.Start()` fails → return `RunResult{Status: StatusFailed, ProviderMeta: meta(nil)}` where `exit_code` is JSON `null`.
7. Two reader goroutines (one per pipe), each using `bufio.Reader.ReadString('\n')` with an accumulating cap of **1 MiB per line** — on overflow emit a truncated frame tagged `"...[truncated]"` and resume at the next newline. Each line → `out <- Frame{Kind: "stdout"|"stderr", Text: line, Index: nextIndex()}` where `nextIndex()` is a single mutex-guarded counter shared by both goroutines. **Only `Frame.Index` monotonicity is guaranteed; the true stdout/stderr interleaving is not preserved.**
8. `wg.Wait()` for both readers, then `err := cmd.Wait()`.
9. Status (checked in this order):
   - `ctx.Err() == context.DeadlineExceeded` → `StatusTimeout`
   - `ctx.Err() == context.Canceled` → `StatusCancelled`
   - `err == nil` → `StatusCompleted`
   - `err` is `*exec.ExitError` → `StatusFailed`, `exit_code = ExitCode()`
   - other `err` → `StatusFailed`, `exit_code = null`
10. `ProviderMeta` = `{"provider": p.name, "argv": args, "exit_code": <int|null>}`.

### 4.3 Runtime discovery (`plugins/agent-harness/discovery.go`)

```go
func discoverProviders(candidates []string, envAllowlist []string, logw io.Writer) map[string]Provider
```

- Always seeds `{"mock": MockProvider{}}`.
- `candidates` default `["codex"]`, overridden by env `VIBE_AGENT_PROVIDERS` (comma-separated executable names).
- For each candidate: `exec.LookPath(name)`; if found, run `<abs> --version` with a **2 s** timeout and the allowlisted env. Exit 0 → register `RealProvider{name, bin: abs, argv: codexArgv, env: allowlistedEnv(envAllowlist), timeout: 0}`. Non-zero / timeout / not-on-PATH → **skip, do not register, do not fail plugin startup**.
- **All discovery logging goes to `logw` = os.Stderr.** Never stdout (that is the `vibe-plugin/1` protocol channel). One line per candidate: `agent-harness: provider "codex" -> /usr/local/bin/codex (registered)` or `... (probe failed: <reason>, skipped)`.

**Env allowlist.** `allowlistedEnv(names)` copies only the named vars from `os.Environ()`. Default list (also the `--version` probe env):

```
PATH HOME USER LOGNAME SHELL LANG LC_ALL LC_CTYPE TERM TMPDIR TZ
SSL_CERT_FILE SSL_CERT_DIR CODEX_HOME OPENAI_API_KEY CODEX_API_KEY
```

Overridable via `VIBE_AGENT_ENV_ALLOWLIST` (comma-separated, **replaces** the default). `FAKE_AGENT_*` and other test variables are **never** on any allowlist — the fake CLI takes everything as argv (§7).

### 4.4 Handler changes (`plugins/agent-harness/handlers.go`, `main.go`)

- `runDeps`: `Prov Provider` → `Providers map[string]Provider` + `DefaultProvider string` + `Runs *runRegistry`.
- `agentRunHandler`:
  1. decode; validate `work_context_id / workspace_path / prompt`.
  2. `name := q.Provider; if name == "" { name = base.DefaultProvider }`.
  3. `prov, ok := base.Providers[name]; if !ok { return INVALID "unknown provider \"<name>\"" }` — **before** any store write. (Deletes the old `"only mock provider is available in M1.3"` branch.)
  4. `runID := protocol.NewID("run")`; `ar.HarnessNativeID = name + "-" + runID`; `ar.Provider = name`; `RecordStarted(ar)`.
  5. `runCtx, runCancel := context.WithCancel(rc.Context())` — same lifetime as today's `go runOnce(rc.Context(), …)` (M1.3 already settled "client disconnect ≠ business cancel" under this context, and it passed review); `runCancel` adds the **explicit** cancel path. `done := make(chan struct{})`.
  6. `base.Runs.register(runID, runCancel, done)`; `go func() { runOnce(runCtx, d, ar, spec, out); base.Runs.done(runID); close(done) }()`.
  7. return `{agent_run, stream_id}`.
- `main.go`: `Providers: discoverProviders(candidatesFromEnv(), allowlistFromEnv(), os.Stderr)`, `DefaultProvider: "mock"`, `Runs: newRunRegistry()`.

### 4.5 Live cancel (`plugins/agent-harness/runreg.go` + `cancelHandler`)

```go
type runRegistry struct {
	mu     sync.Mutex
	cancel map[string]context.CancelFunc
	done   map[string]chan struct{}
}
func (r *runRegistry) register(id string, c context.CancelFunc, done chan struct{})
func (r *runRegistry) stop(id string) (done <-chan struct{}, ok bool)   // calls cancel, returns done chan
func (r *runRegistry) done(id string)                                    // unregister
```

`cancelHandler`:

1. decode `agent_run_id`.
2. `ar, ok := store.GetByID(id)`; not found → `NOT_FOUND`; already terminal → `CONFLICT` (unchanged).
3. `done, live := runs.stop(id)`:
   - **live** (in registry): the provider's `runCtx` is now cancelled. Wait on `done` (bounded, e.g. `p.timeout`+5 s or a fixed 30 s) → `runOnce` runs the provider to its `CANCELLED` return, `blob.put`s the partial transcript, and `Persist`s via the existing `agent.run.completed` op with `Status = "CANCELLED"` + `RawSessionRef`. Then return the final `AgentRun`.
   - **not live** (registry lost it — e.g. after a plugin restart, run still `RUNNING` in the store): append `agent.run.cancelled` (the existing op) to mark it terminal with no transcript, return the `AgentRun`.

**No Store schema change.** Both ops (`agent.run.completed`, `agent.run.cancelled`) already exist; `RecordCompleted` already accepts an arbitrary status string, and `runOnce` already persists `tr.Result.Status` (which the mock already returns as `CANCELLED` on ctx-done).

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

### 6.1 Deterministic (dispatched) — `fixtures/fake-agent-cli`

A committed **Go** `main` package at **`plugins/agent-harness/fakeagentcli/main.go`** (inside the existing `plugins` module — no `go.work` / `go.mod` change), built by `scripts/build.sh` to `.bin/fake-agent-cli` (`go build -o .bin/fake-agent-cli ./plugins/agent-harness/fakeagentcli`). It mimics a coding CLI **taking everything as argv** (so no test env ever touches a production allowlist):

```
fake-agent-cli --version                                             # prints "fake-agent-cli 0.0.1", exit 0
fake-agent-cli --cd <dir> --write <relfile> --line <text> --exit <n> --sleep <ms> -- <prompt...>
```

Behaviour: `--version` short-circuits (for discovery probing). Otherwise: `chdir(--cd)`; print 3 `stdout` lines and one `stderr` line derived from the prompt; if `--write` set, append `--line` + "\n" to `<dir>/<relfile>`; `sleep(--sleep ms)` as an interruptible loop that exits promptly on SIGTERM/SIGKILL; `os.Exit(--exit)`.

`real_provider_test.go` builds a `RealProvider` pointed at `.bin/fake-agent-cli` with a fake `argv` template and asserts:

| case | knobs | expected |
|---|---|---|
| completed + workspace change | `--write Calc.java --line "// hardened" --exit 0` | frames streamed in `Index` order; `Status == COMPLETED`; file contains the line; `exit_code == 0` |
| non-zero exit → FAILED | `--exit 7` | `Status == FAILED`; `ProviderMeta.exit_code == 7` |
| deadline → TIMEOUT | `RealProvider.timeout = 50ms`, `--sleep 5000` | `Status == TIMEOUT` within ~1 s |
| context cancel → CANCELLED | cancel ctx after first frame, `--sleep 5000` | `Status == CANCELLED`; process gone |
| `cmd.Start` failure → FAILED/null | `bin = "/nonexistent"` | `Status == FAILED`; `ProviderMeta.exit_code == null` |
| oversized line | `--line <1.5 MiB>` | a truncated frame containing `[truncated]`, run still `COMPLETED` |

`discovery_test.go`:

| case | expected |
|---|---|
| candidate resolves + `--version` exits 0 (use `.bin/fake-agent-cli --exit 0` as the "binary", `--version` handled) | registered; map has `mock` + that name |
| candidate not on PATH | skipped; map has only `mock`; no error; a stderr log line |
| candidate `--version` exits non-zero | skipped |
| stdout is untouched | capture: `discoverProviders` writes nothing to a stdout spy |

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
| in `RealProvider.Run`, drop the `*exec.ExitError` branch (always `COMPLETED`) | `real_provider_test.go` "non-zero exit → FAILED" |
| drop the `ctx.Err()` checks (map on exit code only) | "deadline → TIMEOUT" and "context cancel → CANCELLED" |
| in `cancelHandler`, skip the wait-for-`done` (return immediately after `stop`) | `handlers_test.go` "live cancel" (final record still `RUNNING` or transcript empty) |
| in `agentRunHandler`, move the unknown-provider check after `RecordStarted` | `handlers_test.go` "bogus" (an orphan `RUNNING` record appears) |
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
