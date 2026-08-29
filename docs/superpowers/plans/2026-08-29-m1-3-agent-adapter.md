# M1.3 — Agent Adapter (mock provider) — Implementation Plan

**Execution:** work task-by-task, in order. Each task: write the failing test first, run it and watch it fail for the right reason, write the minimal implementation, run it green, then (for behavioural changes) briefly invert the production change to confirm the test goes red and restore it. Commit at the end of each task with the message given.

**Goal:** Land `org.vibe.agent.harness` — the provider-neutral plugin that runs a coding-agent session in a worktree, streams its output frames live, and persists a durable `AgentRun` record + raw transcript. This milestone ships only the deterministic **mock provider**; the real CLI provider is M1.8.

**Architecture:** A stateful plugin behind four contracts. `agent.run@1` is a *streaming* command: it records a `RUNNING` AgentRun, returns an accepted response immediately, streams `agent.frame` data over the kernel stream channel, and — after the stream ends — persists the transcript to `org.vibe.blob` and flips the AgentRun to a terminal status. Because the run outlives the request, the plugin holds **`service_authority`** (the kernel's mechanism for autonomous background work) so the post-stream goroutine can still call `blob.put@1`. Persistence uses the same append-only JSONL log + projection pattern as `work-registry` / `workspace`. A `Provider` interface isolates "how an agent runs" — `MockProvider` is the only implementation this milestone.

**Tech Stack:** Go standard library + `kernel/sdk/go/{protocol,pluginhost,fencing}` (via `go.work`, **no new external Go dependency**), newline-delimited JSON over a Unix socket, Python 3 + `jsonschema` for contract checks.

**Spec:** `docs/M1-DESIGN.md` — §3 (chain: `agent.run` streaming, source changes into the worktree, `raw_session_ref` via `blob.put`), §5.1 (agent-adapter consumes `blob.put`), §5.2 (`org.vibe.agent.harness` row), §6 (`AgentRun` shape), §8 (provider neutrality + mock/real dual-track + fault-injection list), §13 milestone M1.3.

**Base:** branch `chatgpt/m1-3-agent-adapter` from `main` at `3bdbe77`. Present: everything through M1.2 — `plugins/foundation/{blob,event-journal}`, `plugins/{work-registry,workspace}`, `cli/vibe`, `scripts/{build,dev-run,smoke,check-arch}.sh` + `scripts/smoke-workspace.sh`, `config/m1-{policy,bindings}.json` with 12 contracts and 4 plugins wired, `architecture-tests/check_composition.py`.

## Global Constraints

- **G1 Kernel Purity:** no task modifies `kernel/` source. M1 code only consumes `kernel/sdk/go/...`. G1 check: `git diff --name-only 3bdbe77 HEAD -- kernel/internal kernel/cmd kernel/sdk` must be empty. If a task seems to need a kernel change, stop and report.
- **Do NOT touch `docs/M1-DESIGN.md`.** Do not stage it, edit it, or commit it. The milestone-status line is updated separately by the reviewer after merge.
- **No new external Go modules.** No `go get`, no `v0.0.0` pseudo-version requires.
- **Module paths:** kernel `github.com/example/agent-native-microkernel`; plugins `github.com/example/agent-native-os/plugins`; CLI `github.com/example/agent-native-os/cli`.
- **Manifest rule:** an export/consume `contract` field MUST equal `<capability>@<major>` exactly. Stateful exports need `mode: "stateful"`, `service`, `authority`; the manifest needs `runtime.data_namespace`. A plugin that makes autonomous (non-delegated) calls needs `"service_authority": true` in its **policy grant** (not the manifest).
- **Contract rule:** `contracts/<dotted.name>/v<major>/schema.json`, `contract` = identity, `kind` ∈ command|query|event (a streaming command is still `kind: "command"`), `version` starting `"<major>."`, Draft 2020-12 `request`/`response`. Register in `contracts/catalog.json`, then `python3 scripts/check-contracts.py --root contracts`.
- **Git identity in tests/scripts:** the environment may have no global git config. Every `git commit` in a test or script must pass identity inline: `git -C <dir> -c user.email=test@example.com -c user.name=test commit ...`.
- **Commit trailer** — every commit message ends with exactly:
  ```
  Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
  Plan: docs/superpowers/plans/2026-08-29-m1-3-agent-adapter.md
  ```
- **Commit identity:** author `ada <oashasu@gmail.com>`.
- **Number discipline:** any count in an expected-output string is whatever the plan's own commands produce; if a literal here differs, trust the commands, adjust the assertion, note it in the final report.

---

## File Structure

New:

- `contracts/agent.run/v1/schema.json`, `contracts/agent.run.get/v1/schema.json`, `contracts/agent.run.query/v1/schema.json`, `contracts/agent.run.cancel/v1/schema.json`
- `plugins/agent-harness/store.go`, `plugins/agent-harness/store_test.go` — `AgentRun` + JSONL log + projection
- `plugins/agent-harness/provider.go`, `plugins/agent-harness/provider_test.go` — `Provider` interface + `MockProvider`
- `plugins/agent-harness/session.go`, `plugins/agent-harness/session_test.go` — `runProvider` orchestration (drives a provider, mirrors frames, returns a transcript)
- `plugins/agent-harness/handlers.go`, `plugins/agent-harness/handlers_test.go` — the four capability handlers
- `plugins/agent-harness/main.go` — wiring
- `plugins/manifests/agent-harness.manifest.json`
- `scripts/smoke-agent.sh` — the M1.3 smoke fragment (invoked from `scripts/smoke.sh`)

Modified:

- `contracts/catalog.json` — +4 entries
- `config/m1-policy.json` — `local-cli` grants + `org.vibe.agent.harness` grant with `service_authority`
- `config/m1-bindings.json` — bindings for the 4 stateful capabilities
- `cli/vibe/wire.go` — add `invokeStream` (accepted response + live frames until stream.close)
- `cli/vibe/main.go` — `agent` subcommand (`run` / `show` / `cancel`)
- `scripts/smoke.sh` — call `scripts/smoke-agent.sh`

Responsibilities: `provider.go` knows only how to run *a* session (emit frames, optionally touch a file, honour cancel); `session.go` drives a provider and mirrors its frames onto an output channel while capturing a transcript; `store.go` owns persistence; `handlers.go` wires kernel envelope ↔ (session + blob + store); `main.go` only wires.

---

## Task 1: agent contracts

**Files:** four schemas; modify `contracts/catalog.json`.

`AgentRun` shape (documented once; schemas keep `agent_run` as a bare object):

```
AgentRun = {
  id, work_context_id, workspace_path, prompt, provider,
  harness_native_id,          # mock: "mock-<run-id>"
  status:                     "RUNNING" | "COMPLETED" | "FAILED" | "CANCELLED" | "TIMEOUT",
  raw_session_ref,            # "" while RUNNING; blob:// URI once terminal
  provider_metadata,          # object, provider-private
  frame_count,
  started_at, ended_at        # ended_at is "" while RUNNING
}
```

`agent.frame` (stream data payload) = `{ "kind": "stdout"|"stderr"|"status", "text": string, "index": integer }`.

- [ ] **Step 1: `contracts/agent.run/v1/schema.json`**

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "contract": "agent.run@1",
  "version": "1.0.0",
  "kind": "command",
  "compatibility": "backward-within-major",
  "request": {
    "type": "object",
    "additionalProperties": false,
    "properties": {
      "work_context_id": { "type": "string" },
      "workspace_path": { "type": "string" },
      "prompt": { "type": "string" },
      "provider": { "type": "string" },
      "mock_steps": { "type": "integer" },
      "mock_delay_ms": { "type": "integer" },
      "mock_fail_at": { "type": "integer" },
      "mock_write_file": { "type": "string" },
      "mock_write_content": { "type": "string" }
    },
    "required": ["work_context_id", "workspace_path", "prompt"]
  },
  "response": {
    "type": "object",
    "additionalProperties": true,
    "properties": {
      "agent_run": { "type": "object" },
      "stream_id": { "type": "string" }
    },
    "required": ["agent_run", "stream_id"]
  }
}
```

(The `mock_*` fields are test/dev knobs; the real provider in M1.8 ignores them. They live in the contract because the mock is a first-class long-lived provider per §8.3.)

- [ ] **Step 2: `contracts/agent.run.get/v1/schema.json`** — `kind` `"query"`, request `{ "agent_run_id": string }` required, response `{ "agent_run": object }` required.

- [ ] **Step 3: `contracts/agent.run.query/v1/schema.json`** — `kind` `"query"`, request `{ "work_context_id": string }` required, response `{ "agent_runs": { "type": "array" } }` required.

- [ ] **Step 4: `contracts/agent.run.cancel/v1/schema.json`** — `kind` `"command"`, request `{ "agent_run_id": string }` required, response `{ "agent_run": object }` required.

- [ ] **Step 5: catalog + check**

Add to `contracts/catalog.json`:

```json
  "agent.run@1": "agent.run/v1/schema.json",
  "agent.run.get@1": "agent.run.get/v1/schema.json",
  "agent.run.query@1": "agent.run.query/v1/schema.json",
  "agent.run.cancel@1": "agent.run.cancel/v1/schema.json"
```

Run: `python3 scripts/check-contracts.py --root contracts` → `CONTRACT CHECK: PASSED` (count = entries now in catalog.json: 12 + 4 = 16).

- [ ] **Step 6: commit**

```
build(m1.3): agent.run/get/query/cancel contracts
```

---

## Task 2: agent store + projection

**Files:** `plugins/agent-harness/store.go`, `plugins/agent-harness/store_test.go`.

**Interfaces:** `type Store`, `Load(dir string) (*Store, error)` (opens `dir/agent-log.jsonl`, replays), `RecordStarted(ar AgentRun) error`, `RecordCompleted(id, status, rawRef string, frameCount int, meta json.RawMessage) error`, `RecordCancelled(id string) error`, `GetByID(id string) (AgentRun, bool)`, `QueryByContext(wcID string) []AgentRun` (chronological). Types: `AgentRun`, status string constants `StatusRunning/StatusCompleted/StatusFailed/StatusCancelled/StatusTimeout`, `ErrNotFound`. Same JSONL-log + single `apply` reducer + fsync-per-append + torn-last-line tolerance as `plugins/work-registry/store.go` — read that file and mirror it.

Terminal-status rule enforced in the store: `RecordCompleted` / `RecordCancelled` on a run that is already terminal return `ErrAlreadyTerminal` (a distinct sentinel) — the reducer must be idempotent-safe on replay but the live methods reject a second terminal transition.

- [ ] **Step 1: write the failing test**

`plugins/agent-harness/store_test.go`:

```go
package main

import (
	"encoding/json"
	"testing"
)

func TestRecordStartedThenCompleted(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	ar := AgentRun{ID: "run-1", WorkContextID: "wc-1", Prompt: "p", Provider: "mock", Status: StatusRunning, StartedAt: "t0"}
	if err := s.RecordStarted(ar); err != nil {
		t.Fatalf("started: %v", err)
	}
	if err := s.RecordCompleted("run-1", StatusCompleted, "blob://sha256/abc", 3, json.RawMessage(`{"k":1}`)); err != nil {
		t.Fatalf("completed: %v", err)
	}
	got, ok := s.GetByID("run-1")
	if !ok || got.Status != StatusCompleted || got.RawSessionRef != "blob://sha256/abc" || got.FrameCount != 3 {
		t.Fatalf("run: %+v ok=%v", got, ok)
	}
}

func TestSecondTerminalTransitionRejected(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_ = s.RecordStarted(AgentRun{ID: "run-1", Status: StatusRunning})
	_ = s.RecordCompleted("run-1", StatusCompleted, "", 0, nil)
	if err := s.RecordCancelled("run-1"); err == nil {
		t.Fatal("cancelling an already-completed run must error")
	}
}

func TestProjectionRebuildsAndQueryByContext(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_ = s.RecordStarted(AgentRun{ID: "r1", WorkContextID: "wc-1", Status: StatusRunning, StartedAt: "t1"})
	_ = s.RecordStarted(AgentRun{ID: "r2", WorkContextID: "wc-1", Status: StatusRunning, StartedAt: "t2"})
	_ = s.RecordCompleted("r1", StatusCompleted, "", 1, nil)

	reloaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	runs := reloaded.QueryByContext("wc-1")
	if len(runs) != 2 || runs[0].ID != "r1" || runs[0].Status != StatusCompleted || runs[1].ID != "r2" {
		t.Fatalf("query: %+v", runs)
	}
}
```

- [ ] **Step 2: run to verify failure.**

- [ ] **Step 3: implement `store.go`** mirroring `plugins/work-registry/store.go`:

- `AgentRun` struct, json tags per Task 1 (`id`, `work_context_id`, `workspace_path`, `prompt`, `provider`, `harness_native_id`, `status`, `raw_session_ref`, `provider_metadata` as `json.RawMessage`, `frame_count`, `started_at`, `ended_at`).
- `logRecord { Seq int64; TS string; Op string; Data json.RawMessage }`, ops `"agent.run.started"`, `"agent.run.completed"`, `"agent.run.cancelled"`.
- `Store { mu sync.Mutex; path string; seq int64; byID map[string]*AgentRun; byCtx map[string][]string }` (byCtx keeps insertion order per context).
- `Load` / `apply` / `append` exactly like work-registry (torn-last-line tolerance; propagate write/sync error even if Close succeeds; `apply` used by both replay and live).
- `apply` `"agent.run.started"` → unmarshal `AgentRun`, store copy, append id to `byCtx[wc]`. `"agent.run.completed"` → unmarshal `{id,status,raw_session_ref,frame_count,provider_metadata,ended_at}`, look up (→ `ErrNotFound`), set fields. `"agent.run.cancelled"` → `{id,ended_at}`, set `Status=StatusCancelled`, `EndedAt`.
- Live `RecordCompleted` / `RecordCancelled`: before `append`, if `byID[id].Status` is already terminal (not `StatusRunning`) → return `ErrAlreadyTerminal` (do not append). During replay `apply` does not re-check (a log is authoritative).
- `QueryByContext` returns copies in `byCtx` order.
- `ErrNotFound`, `ErrAlreadyTerminal` sentinels; `cloneRun` for every read.

- [ ] **Step 4: run the tests green.**

- [ ] **Step 5: mutation check** — in `RecordCompleted`/`RecordCancelled`, drop the "already terminal" guard. Run `go test ./plugins/agent-harness/... -run TestSecondTerminalTransitionRejected` → FAIL. Restore.

- [ ] **Step 6: commit**

```
feat(m1.3): agent-harness store — JSONL log + projection
```

---

## Task 3: `Provider` interface + `MockProvider`

**Files:** `plugins/agent-harness/provider.go`, `plugins/agent-harness/provider_test.go`.

**Interfaces:**

```go
type Frame struct {
	Kind  string `json:"kind"`  // "stdout" | "stderr" | "status"
	Text  string `json:"text"`
	Index int    `json:"index"`
}

type RunSpec struct {
	WorkspacePath string
	Prompt        string
	MockSteps     int
	MockDelayMS   int
	MockFailAt    int    // 0 = never
	MockWriteFile string // relative to WorkspacePath; "" = no file change
	MockWriteContent string
}

type RunResult struct {
	Status       string          // StatusCompleted | StatusFailed | StatusCancelled | StatusTimeout
	NativeID     string
	ProviderMeta json.RawMessage
}

// Provider runs one agent session. It sends Frames on `out` and closes it,
// then the final RunResult is available on the returned channel.
type Provider interface {
	Name() string
	Run(ctx context.Context, spec RunSpec, out chan<- Frame) RunResult
}
```

`MockProvider` (this milestone's only implementation):
- emits `MockSteps` (default 3) `stdout` frames `"step-<i>: <prompt-first-40-chars>"`, sleeping `MockDelayMS` (default 5) between them;
- if `MockWriteFile != ""`, after the first frame, `os.MkdirAll` the parent and **append** `MockWriteContent` (default `"// touched by mock agent\n"`) to `WorkspacePath/MockWriteFile` — this simulates the agent editing code, so M1.4 has a diff to collect;
- if `MockFailAt > 0` and the loop reaches that step, emit a `stderr` frame `"mock failure at step N"` and return `RunResult{Status: StatusFailed}`;
- on `ctx.Done()` before finishing, return `RunResult{Status: StatusCancelled}`;
- otherwise return `RunResult{Status: StatusCompleted, NativeID: "mock-" + <something>, ProviderMeta: {"steps": N}}`.
- `Run` always `close(out)` before returning (via `defer`).

- [ ] **Step 1: write the failing test**

`plugins/agent-harness/provider_test.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func drain(out <-chan Frame) []Frame {
	var fs []Frame
	for f := range out {
		fs = append(fs, f)
	}
	return fs
}

func TestMockProviderEmitsFramesAndTouchesWorkspace(t *testing.T) {
	ws := t.TempDir()
	p := MockProvider{}
	out := make(chan Frame, 16)
	var res RunResult
	go func() { res = p.Run(context.Background(), RunSpec{WorkspacePath: ws, Prompt: "harden add", MockSteps: 4, MockDelayMS: 1, MockWriteFile: "src/Calc.java", MockWriteContent: "// x\n"}, out); _ = json.Marshal }()
	fs := drain(out)
	if len(fs) != 4 || fs[0].Kind != "stdout" || fs[3].Index != 4 {
		t.Fatalf("frames: %+v", fs)
	}
	time.Sleep(20 * time.Millisecond)
	if res.Status != StatusCompleted {
		t.Fatalf("result: %+v", res)
	}
	b, err := os.ReadFile(filepath.Join(ws, "src/Calc.java"))
	if err != nil || string(b) != "// x\n" {
		t.Fatalf("workspace file: %q err=%v", b, err)
	}
}

func TestMockProviderFailAt(t *testing.T) {
	p := MockProvider{}
	out := make(chan Frame, 16)
	res := p.Run(context.Background(), RunSpec{WorkspacePath: t.TempDir(), Prompt: "p", MockSteps: 5, MockDelayMS: 1, MockFailAt: 3}, out)
	if res.Status != StatusFailed {
		t.Fatalf("expected FAILED, got %+v", res)
	}
}

func TestMockProviderCancel(t *testing.T) {
	p := MockProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan Frame, 1)
	done := make(chan RunResult, 1)
	go func() { done <- p.Run(ctx, RunSpec{WorkspacePath: t.TempDir(), Prompt: "p", MockSteps: 50, MockDelayMS: 20}, out) }()
	<-out // first frame
	cancel()
	select {
	case res := <-done:
		if res.Status != StatusCancelled {
			t.Fatalf("expected CANCELLED, got %+v", res)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("provider did not stop on cancel")
	}
}
```

- [ ] **Step 2: run to verify failure.**

- [ ] **Step 3: implement `provider.go`.**

- [ ] **Step 4: run green.**

- [ ] **Step 5: mutation check** — remove the `ctx.Done()` case from the `MockProvider` loop's `select`. Run `go test ./plugins/agent-harness/... -run TestMockProviderCancel` → FAIL (times out). Restore.

- [ ] **Step 6: commit**

```
feat(m1.3): Provider interface + deterministic MockProvider
```

---

## Task 4: `runProvider` orchestration

**Files:** `plugins/agent-harness/session.go`, `plugins/agent-harness/session_test.go`.

**Interfaces:**

```go
type Transcript struct {
	Frames []Frame
	Result RunResult
}

func (t Transcript) Bytes() []byte   // newline-delimited JSON of each frame, then a final "== result: <status> ==" line

// runProvider drives prov, mirrors every frame onto `mirror` (best-effort, non-blocking
// past a bounded wait so a slow consumer cannot wedge the run), captures the full
// transcript, closes `mirror`, and returns the Transcript.
func runProvider(ctx context.Context, prov Provider, spec RunSpec, mirror chan<- any) Transcript
```

- [ ] **Step 1: write the failing test**

`plugins/agent-harness/session_test.go`:

```go
package main

import (
	"context"
	"strings"
	"testing"
)

func TestRunProviderMirrorsAndCaptures(t *testing.T) {
	mirror := make(chan any, 16)
	var tr Transcript
	done := make(chan struct{})
	go func() { tr = runProvider(context.Background(), MockProvider{}, RunSpec{WorkspacePath: t.TempDir(), Prompt: "p", MockSteps: 3, MockDelayMS: 1}, mirror); close(done) }()

	var mirrored int
	for range mirror {
		mirrored++
	}
	<-done
	if mirrored != 3 {
		t.Fatalf("mirrored %d frames, want 3", mirrored)
	}
	if len(tr.Frames) != 3 || tr.Result.Status != StatusCompleted {
		t.Fatalf("transcript: %+v", tr)
	}
	body := string(tr.Bytes())
	if strings.Count(body, "\n") < 3 || !strings.Contains(body, "result: COMPLETED") {
		t.Fatalf("transcript bytes:\n%s", body)
	}
}

func TestRunProviderClosesMirrorEvenOnFailure(t *testing.T) {
	mirror := make(chan any, 16)
	tr := runProvider(context.Background(), MockProvider{}, RunSpec{WorkspacePath: t.TempDir(), Prompt: "p", MockSteps: 5, MockDelayMS: 1, MockFailAt: 2}, mirror)
	// mirror must be closed (range returns) and the transcript records FAILED.
	for range mirror {
	}
	if tr.Result.Status != StatusFailed {
		t.Fatalf("want FAILED, got %+v", tr.Result)
	}
}
```

- [ ] **Step 2: run to verify failure.**

- [ ] **Step 3: implement `session.go`.** `runProvider` starts `prov.Run` in a goroutine writing to an internal `chan Frame`; ranges that channel, appending to `tr.Frames` and forwarding each frame to `mirror` with a bounded `select { case mirror <- f: case <-time.After(2*time.Second): }` (a wedged consumer drops the mirror copy but the run and transcript continue); collects `RunResult`; `close(mirror)`; returns. `Transcript.Bytes()` marshals each frame as a JSON line then appends `"== result: " + Result.Status + " ==\n"`.

- [ ] **Step 4: run green.**

- [ ] **Step 5: mutation check** — remove the `close(mirror)` call. Run `go test ./plugins/agent-harness/... -run TestRunProviderClosesMirrorEvenOnFailure` → FAIL (the `for range mirror` hangs → test times out). Restore.

- [ ] **Step 6: commit**

```
feat(m1.3): runProvider — mirror frames + capture transcript
```

---

## Task 5: `agent.run` streaming handler

**Files:** `plugins/agent-harness/handlers.go`, `plugins/agent-harness/handlers_test.go`.

**Interfaces:**

```go
type runDeps struct {
	Store   *Store
	Prov    Provider
	BlobPut func(payload []byte) (uri string, err error)   // prod: autonomous h.Command("blob.put", 1, ...)
	Persist func(runID, status, rawRef string, frames int, meta json.RawMessage) error // prod: fencing.WithWriteFence(e, store.RecordCompleted)
	Now     func() string
}

func agentRunHandler(d runDeps) pluginhost.ContextHandler
```

Flow: parse + validate (`work_context_id`, `workspace_path`, `prompt` required) → build `AgentRun{Status: RUNNING, HarnessNativeID: "mock-"+runID}` → `d.Persist` is NOT used for the start; record started with `fencing.WithWriteFence(e, func() error { return d.Store.RecordStarted(ar) })` (delegation still alive here) → `out := make(chan any, 64)` → `go func(){ tr := runProvider(rc.Context(), d.Prov, spec, out); uri, _ := d.BlobPut(tr.Bytes()); _ = d.Persist(runID, tr.Result.Status, uri, len(tr.Frames), tr.Result.ProviderMeta) }()` → `acc := rc.Stream(out)` → return `{agent_run: ar, stream_id: acc.StreamID}`.

- [ ] **Step 1: write the failing test**

`plugins/agent-harness/handlers_test.go`:

```go
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

func fencedEnv(t *testing.T, dir string) protocol.Envelope {
	t.Helper()
	fenceRoot := filepath.Join(dir, ".fences")
	_ = os.MkdirAll(fenceRoot, 0o755)
	t.Setenv("VIBE_DATA_DIR", dir)
	t.Setenv("VIBE_RUNTIME_ID", "rt-test")
	t.Setenv("VIBE_FENCE_ROOT", fenceRoot)
	lease := map[string]any{"service": "default-agent-harness", "authority": "agent-runs-main", "runtime_id": "rt-test", "epoch": 1}
	b, _ := json.Marshal(lease)
	_ = os.WriteFile(filepath.Join(fenceRoot, "default-agent-harness--agent-runs-main.json"), b, 0o644)
	return protocol.Envelope{Protocol: 1, MessageID: "m", Kind: protocol.KindCommand, Service: "default-agent-harness", Authority: "agent-runs-main", FencingEpoch: 1}
}

// The handler test drives agentRunHandler through a real RequestContext by using the
// pluginhost test seam: build a Host with the handler registered, feed it a command
// envelope over an in-memory pipe, and read the frames + accepted response back.
// Simpler: call the exported runOnce helper that agentRunHandler delegates to.
func TestRunOncePersistsTerminalRun(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	ws := t.TempDir()
	var putBytes []byte
	var mu sync.Mutex
	deps := runDeps{
		Store: s, Prov: MockProvider{}, Now: func() string { return "t0" },
		BlobPut: func(b []byte) (string, error) { mu.Lock(); putBytes = b; mu.Unlock(); return "blob://sha256/deadbeef", nil },
		Persist: func(id, status, ref string, n int, meta json.RawMessage) error {
			return s.RecordCompleted(id, status, ref, n, meta)
		},
	}
	ar := AgentRun{ID: "run-x", WorkContextID: "wc-1", WorkspacePath: ws, Prompt: "p", Provider: "mock", Status: StatusRunning, StartedAt: "t0"}
	if err := s.RecordStarted(ar); err != nil {
		t.Fatal(err)
	}
	out := make(chan any, 64)
	done := make(chan struct{})
	go func() { runOnce(deps, ar, RunSpec{WorkspacePath: ws, Prompt: "p", MockSteps: 3, MockDelayMS: 1, MockWriteFile: "f.txt", MockWriteContent: "z\n"}, out); close(done) }()
	for range out {
	}
	<-done

	got, _ := s.GetByID("run-x")
	if got.Status != StatusCompleted || got.RawSessionRef != "blob://sha256/deadbeef" || got.FrameCount != 3 {
		t.Fatalf("terminal run: %+v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(putBytes) == 0 {
		t.Fatal("transcript was not sent to blob.put")
	}
	if b, _ := os.ReadFile(filepath.Join(ws, "f.txt")); string(b) != "z\n" {
		t.Fatalf("workspace change missing: %q", b)
	}
	_ = pluginhost.Host{}
	_ = fencedEnv
	_ = time.Second
}

func TestAgentRunHandlerRejectsMissingFields(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	deps := runDeps{Store: s, Prov: MockProvider{}, BlobPut: func([]byte) (string, error) { return "", nil }, Persist: func(string, string, string, int, json.RawMessage) error { return nil }, Now: func() string { return "t0" }}
	h := agentRunHandler(deps)
	_, perr := h(&pluginhost.RequestContext{}, protocol.Envelope{Payload: protocol.NewPayload(map[string]string{"prompt": "p"})})
	if perr == nil || perr.Code != "INVALID" {
		t.Fatalf("want INVALID, got %+v", perr)
	}
}
```

Note: `agentRunHandler` must delegate the goroutine body to an exported-in-package helper `runOnce(d runDeps, ar AgentRun, spec RunSpec, out chan any)` so it is unit-testable without a live `RequestContext`. `agentRunHandler` itself: validate, `RecordStarted` under fence, `go runOnce(...)`, `rc.Stream(out)`, return accepted. Calling `h(&pluginhost.RequestContext{}, env)` with an empty context is only exercised for the validation path (which returns before touching `rc`).

- [ ] **Step 2: run to verify failure.**

- [ ] **Step 3: implement `handlers.go`** (`agentRunHandler` + `runOnce` only for this task). `runOnce`:

```go
func runOnce(d runDeps, ar AgentRun, spec RunSpec, out chan any) {
	tr := runProvider(context.Background(), d.Prov, spec, out)
	uri, err := d.BlobPut(tr.Bytes())
	if err != nil {
		uri = "" // record the terminal status anyway; the transcript ref is best-effort in M1
	}
	_ = d.Persist(ar.ID, tr.Result.Status, uri, len(tr.Frames), tr.Result.ProviderMeta)
}
```

(`runProvider` takes `chan<- any`; pass `out`. The ctx here is `context.Background()` for the unit-test helper; the real handler passes `rc.Context()` — see Task 6 wiring. Accept a `ctx context.Context` param on `runOnce` and pass it through so cancel works end to end.)

Adjust the test to pass `context.Background()` as the first arg.

- [ ] **Step 4: run green.**

- [ ] **Step 5: mutation check** — in `runOnce`, skip the `d.Persist(...)` call. Run `go test ./plugins/agent-harness/... -run TestRunOncePersistsTerminalRun` → FAIL (`got.Status` still `RUNNING`). Restore.

- [ ] **Step 6: commit**

```
feat(m1.3): agent.run streaming handler + runOnce persistence
```

---

## Task 6: get / query / cancel handlers + plugin wiring

**Files:** modify `plugins/agent-harness/handlers.go`, `plugins/agent-harness/handlers_test.go`; create `plugins/agent-harness/main.go`, `plugins/manifests/agent-harness.manifest.json`.

- [ ] **Step 1: write the failing tests** (append):

```go
func TestGetAndQueryHandlers(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_ = s.RecordStarted(AgentRun{ID: "r1", WorkContextID: "wc-1", Status: StatusRunning, StartedAt: "t1"})
	_ = s.RecordCompleted("r1", StatusCompleted, "blob://sha256/x", 2, nil)

	gout, perr := getHandler(s)(protocol.Envelope{Payload: protocol.NewPayload(map[string]string{"agent_run_id": "r1"})})
	if perr != nil {
		t.Fatalf("get: %+v", perr)
	}
	gb, _ := json.Marshal(gout)
	var gr struct{ AgentRun AgentRun `json:"agent_run"` }
	_ = json.Unmarshal(gb, &gr)
	if gr.AgentRun.Status != StatusCompleted {
		t.Fatalf("get returned: %+v", gr.AgentRun)
	}

	qout, _ := queryHandler(s)(protocol.Envelope{Payload: protocol.NewPayload(map[string]string{"work_context_id": "wc-1"})})
	qb, _ := json.Marshal(qout)
	var qr struct{ AgentRuns []AgentRun `json:"agent_runs"` }
	_ = json.Unmarshal(qb, &qr)
	if len(qr.AgentRuns) != 1 {
		t.Fatalf("query returned %d runs", len(qr.AgentRuns))
	}

	_, perr = getHandler(s)(protocol.Envelope{Payload: protocol.NewPayload(map[string]string{"agent_run_id": "nope"})})
	if perr == nil || perr.Code != "NOT_FOUND" {
		t.Fatalf("want NOT_FOUND, got %+v", perr)
	}
}

func TestCancelHandlerMarksRunCancelled(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	env := fencedEnv(t, dir)
	_ = s.RecordStarted(AgentRun{ID: "r1", Status: StatusRunning, StartedAt: "t1"})
	env.Payload = protocol.NewPayload(map[string]string{"agent_run_id": "r1"})
	out, perr := cancelHandler(s)(env)
	if perr != nil {
		t.Fatalf("cancel: %+v", perr)
	}
	_ = out
	got, _ := s.GetByID("r1")
	if got.Status != StatusCancelled {
		t.Fatalf("run not cancelled: %+v", got)
	}
	// cancelling a terminal run is a CONFLICT
	_, perr = cancelHandler(s)(env)
	if perr == nil || perr.Code != "CONFLICT" {
		t.Fatalf("second cancel: %+v", perr)
	}
}
```

- [ ] **Step 2: run to verify failure.**

- [ ] **Step 3: implement `getHandler` / `queryHandler` / `cancelHandler`.**
- `getHandler(s)`: `{agent_run_id}` required → `s.GetByID` → `NOT_FOUND` → `{agent_run}`.
- `queryHandler(s)`: `{work_context_id}` required → `s.QueryByContext` → `{agent_runs: [...]}` (always an array, `[]AgentRun{}` if none).
- `cancelHandler(s)`: `{agent_run_id}` required; fenced; `s.RecordCancelled(id)`; map `ErrNotFound → NOT_FOUND`, `ErrAlreadyTerminal → CONFLICT`; return `{agent_run: updated}`. (M1.3: cancel only flips the record; interrupting the actual in-flight goroutine via the kernel's cancel plumbing is M1.8 with the real provider. Note this in the report.)

- [ ] **Step 4: write `main.go`**

```go
package main

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/example/agent-native-microkernel/sdk/go/fencing"
	"github.com/example/agent-native-microkernel/sdk/go/pluginhost"
	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

func main() {
	dir := os.Getenv("VIBE_DATA_DIR")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		panic(err)
	}
	s, err := Load(dir)
	if err != nil {
		panic("agent-harness load: " + err.Error())
	}
	h := pluginhost.New("org.vibe.agent.harness", "1.0.0", "")

	deps := runDeps{
		Store: s, Prov: MockProvider{}, Now: func() string { return time.Now().UTC().Format(time.RFC3339Nano) },
		BlobPut: func(payload []byte) (string, error) {
			resp, cerr := h.Command("blob.put", 1, map[string]string{"content_base64": b64(payload)}, 30*time.Second)
			if cerr != nil {
				return "", cerr
			}
			var r struct{ URI string `json:"uri"` }
			_ = json.Unmarshal(resp.Payload, &r)
			return r.URI, nil
		},
	}
	// Persist closes over the request envelope so the post-stream write is fenced.
	// agentRunHandler builds it per-request; see below.
	h.HandleContextCommand("agent.run", 1, agentRunHandler(deps))
	h.HandleQuery("agent.run.get", 1, getHandler(s))
	h.HandleQuery("agent.run.query", 1, queryHandler(s))
	h.HandleContextCommand("agent.run.cancel", 1, wrap(cancelHandler(s)))
	_ = fencing.WithWriteFence
	_ = context.Background
	_ = h.Serve()
}

func wrap(fn pluginhost.Handler) pluginhost.ContextHandler {
	return func(_ *pluginhost.RequestContext, e protocol.Envelope) (any, *protocol.Error) { return fn(e) }
}
```

`agentRunHandler` builds `d.Persist` per request:

```go
func agentRunHandler(base runDeps) pluginhost.ContextHandler {
	return func(rc *pluginhost.RequestContext, e protocol.Envelope) (any, *protocol.Error) {
		// ...validate, build ar, RecordStarted under fencing.WithWriteFence(e, ...)
		d := base
		d.Persist = func(id, status, ref string, n int, meta json.RawMessage) error {
			return fencing.WithWriteFence(e, func() error { return base.Store.RecordCompleted(id, status, ref, n, meta) })
		}
		out := make(chan any, 64)
		go runOnce(rc.Context(), d, ar, spec, out)
		acc := rc.Stream(out)
		return map[string]any{"agent_run": ar, "stream_id": acc.StreamID}, nil
	}
}
```

Add a `b64` helper (`encoding/base64.StdEncoding.EncodeToString`).

- [ ] **Step 5: `plugins/manifests/agent-harness.manifest.json`**

```json
{
  "manifest_version": 1,
  "plugin": { "id": "org.vibe.agent.harness", "version": "1.0.0" },
  "runtime": {
    "protocol": "vibe-plugin/1",
    "executable": "../bin/agent-harness",
    "isolation": "process",
    "data_namespace": "state-authority/agent-runs-main"
  },
  "exports": [
    { "capability": "agent.run", "major": 1, "contract": "agent.run@1", "mode": "stateful", "service": "default-agent-harness", "authority": "agent-runs-main", "priority": 100 },
    { "capability": "agent.run.get", "major": 1, "contract": "agent.run.get@1", "mode": "stateful", "service": "default-agent-harness", "authority": "agent-runs-main", "priority": 100 },
    { "capability": "agent.run.query", "major": 1, "contract": "agent.run.query@1", "mode": "stateful", "service": "default-agent-harness", "authority": "agent-runs-main", "priority": 100 },
    { "capability": "agent.run.cancel", "major": 1, "contract": "agent.run.cancel@1", "mode": "stateful", "service": "default-agent-harness", "authority": "agent-runs-main", "priority": 100 }
  ],
  "consumes": {
    "required": [
      { "capability": "blob.put", "major": 1, "contract": "blob.put@1" }
    ]
  },
  "permissions": ["exec:agent", "fs:write"],
  "restart": { "mode": "on_failure", "max_attempts": 2, "cooldown_ms": 200 },
  "resources": { "memory_mb": 256, "cpu_weight": 30 }
}
```

- [ ] **Step 6: build + composition check**

Run: `cd <repo-root> && bash scripts/build.sh` → expect `built plugin: agent-harness`, `BUILD OK`.
Run: `python3 architecture-tests/check_composition.py` → `COMPOSITION FITNESS: PASSED` (manifest count now 5; `agent-harness` consumes 1 capability, well under the warn threshold).

- [ ] **Step 7: commit**

```
feat(m1.3): agent.run.get/query/cancel + plugin wiring (service_authority + blob.put)
```

---

## Task 7: policy, bindings, `vibe agent`, smoke

**Files:** modify `config/m1-policy.json`, `config/m1-bindings.json`, `cli/vibe/wire.go`, `cli/vibe/main.go`, `scripts/smoke.sh`; create `scripts/smoke-agent.sh`.

- [ ] **Step 1: policy + bindings**

`config/m1-policy.json` — add to `grants.local-cli.capabilities`:

```json
  "agent.run@1",
  "agent.run.get@1",
  "agent.run.query@1",
  "agent.run.cancel@1"
```

Add `grants."org.vibe.agent.harness"`:

```json
  "org.vibe.agent.harness": {
    "capabilities": ["blob.put@1"],
    "service_authority": true
  }
```

(`service_authority` lets the post-stream goroutine call `blob.put@1` autonomously after the request delegation is torn down. `blob.put@1` in `capabilities` is the plugin's own grant required for that call.)

`config/m1-bindings.json` — add:

```json
{ "capability": "agent.run", "major": 1, "service": "default-agent-harness", "authority": "agent-runs-main" },
{ "capability": "agent.run.get", "major": 1, "service": "default-agent-harness", "authority": "agent-runs-main" },
{ "capability": "agent.run.query", "major": 1, "service": "default-agent-harness", "authority": "agent-runs-main" },
{ "capability": "agent.run.cancel", "major": 1, "service": "default-agent-harness", "authority": "agent-runs-main" }
```

- [ ] **Step 2: `invokeStream` in `cli/vibe/wire.go`**

Add a function that dials, sends the `WireRequest` with a `stream_id` set on the envelope, reads the accepted response, then reads and yields frame envelopes until one with `Kind == protocol.KindStreamClose`:

```go
func invokeStream(socket, identity, token string, req protocol.Envelope, onFrame func(protocol.Envelope)) (protocol.Envelope, error) {
	c, err := net.DialTimeout("unix", socket, 5*time.Second)
	if err != nil {
		return protocol.Envelope{}, err
	}
	defer c.Close()
	req.Caller, req.Principal, req.ActorChain, req.DelegationID = "", "", nil, ""
	if req.Protocol == 0 { req.Protocol = 1 }
	if req.MessageID == "" { req.MessageID = protocol.NewID("cli") }
	if req.StreamID == "" { req.StreamID = protocol.NewID("stream") }
	if req.TraceID == "" { req.TraceID = protocol.NewID("trace") }
	if req.CorrelationID == "" { req.CorrelationID = protocol.NewID("corr") }
	enc := json.NewEncoder(c)
	if err := enc.Encode(wireRequest{Identity: identity, Token: token, Envelope: req}); err != nil {
		return protocol.Envelope{}, err
	}
	dec := json.NewDecoder(c)
	var accepted protocol.Envelope
	if err := dec.Decode(&accepted); err != nil {
		return protocol.Envelope{}, err
	}
	if accepted.Kind == protocol.KindError && accepted.Error != nil {
		return accepted, fmt.Errorf("%s: %s", accepted.Error.Code, accepted.Error.Message)
	}
	for {
		var f protocol.Envelope
		if err := dec.Decode(&f); err != nil {
			return accepted, nil // stream ended / connection closed
		}
		if onFrame != nil {
			onFrame(f)
		}
		if f.Kind == protocol.KindStreamClose {
			return accepted, nil
		}
	}
}
```

- [ ] **Step 3: `vibe agent` subcommand** in `cli/vibe/main.go`

- `vibe agent run <work-context-id> -workspace <path> -prompt "<text>" [-steps N] [-fail-at N] [-write-file <rel>] [-write-content <s>]` → `invokeStream` with an `agent.run@1` command; print each `stream.data` frame's `text` prefixed with `» `; on `stream.close` print the accepted `agent_run` id + `stream_id`; then poll `agent.run.get` once and print the terminal `status` + `raw_session_ref` (wait/retry up to ~5s for the goroutine to persist).
- `vibe agent show <agent-run-id>` → `agent.run.get@1`; print `id / status / provider / frame_count / raw_session_ref`, `-json` for raw.
- `vibe agent cancel <agent-run-id>` → `agent.run.cancel@1`; print `status`.

- [ ] **Step 4: write `scripts/smoke-agent.sh`**

```bash
#!/usr/bin/env bash
# M1.3 smoke fragment: run a mock agent session against a real worktree, verify the
# AgentRun goes terminal, the transcript blob resolves, the workspace file changed,
# and the run survives a kernel restart. Assumes SOCK/DATA/TOKEN exported and a
# restart_kernel function (or inline the restart).
set -euo pipefail
V=".bin/vibe -socket $SOCK -identity local-cli -token $TOKEN"
RAW=".bin/vibe-raw -socket $SOCK -identity local-cli -token $TOKEN"

SRC="$DATA/agentsrc"
mkdir -p "$SRC"
git -C "$SRC" -c init.defaultBranch=main init -q
echo base > "$SRC/App.java"
git -C "$SRC" add -A
git -C "$SRC" -c user.email=smoke@test -c user.name=smoke -c commit.gpgsign=false commit -q -m init

WC_ID="$($V task create -title "agent smoke" -goal g -repo "$SRC" | sed -n 's/.*wc \([^ ]*\).*/\1/p')"
WT="$($V workspace allocate "$WC_ID" -repo "$SRC" | sed -n 's/.*path \([^ ]*\).*/\1/p')"
[ -d "$WT" ] || { echo "FAIL: no worktree"; exit 1; }

run_out="$($V agent run "$WC_ID" -workspace "$WT" -prompt "touch App" -steps 3 -write-file App.java -write-content '// agent was here
')"
RUN_ID="$(echo "$run_out" | sed -n 's/.*agent_run \([^ ]*\).*/\1/p; s/.*run \([^ ]*\).*/\1/p' | head -1)"
[ -n "$RUN_ID" ] || { echo "FAIL: no run id: $run_out"; exit 1; }

# Wait for the post-stream goroutine to persist a terminal status.
for _ in $(seq 1 50); do
  st="$($V agent show "$RUN_ID" | sed -n 's/.*status \([A-Z]*\).*/\1/p')"
  [ "$st" = "COMPLETED" ] && break
  sleep 0.1
done
[ "$st" = "COMPLETED" ] || { echo "FAIL: run status = $st"; cat "$DATA/kernel.log"; exit 1; }

REF="$($V agent show "$RUN_ID" -json | sed -n 's/.*"raw_session_ref":"\([^"]*\)".*/\1/p')"
echo "$REF" | grep -q '^blob://sha256/' || { echo "FAIL: raw_session_ref = $REF"; exit 1; }
$RAW -cap blob.get -kind query -service default-blob -authority blob-main -payload "{\"uri\":\"$REF\"}" | grep -q 'content_base64' \
  || { echo "FAIL: transcript blob not resolvable"; exit 1; }

grep -q 'agent was here' "$WT/App.java" || { echo "FAIL: mock agent did not change the worktree file"; exit 1; }

restart_kernel
$V agent show "$RUN_ID" | grep -q 'COMPLETED' || { echo "FAIL: run lost on restart"; exit 1; }

echo "M1.3 AGENT SMOKE: OK"
```

- [ ] **Step 5: wire into `scripts/smoke.sh`** — before the final `echo "M1 SMOKE: PASSED"`, add `source scripts/smoke-agent.sh` (after the workspace fragment). Ensure `restart_kernel` is available (it was introduced in M1.2 Task 6; if it was inlined there instead, inline the restart in `smoke-agent.sh` the same way).

- [ ] **Step 6: run the smoke** — `bash scripts/smoke.sh` → `M1.3 AGENT SMOKE: OK` then `M1 SMOKE: PASSED`. Common failures: `service_authority` missing from the `org.vibe.agent.harness` grant (blob.put denied in the goroutine → `raw_session_ref` stays empty), missing binding, `local-cli` missing an `agent.run*@1` grant, the CLI not waiting long enough for the async persist.

- [ ] **Step 7: commit**

```
feat(m1.3): M1 policy/bindings for agent-harness, vibe agent subcommand, streaming smoke
```

---

## Task 8: acceptance gate + PR

**Files:** none — **do not touch `docs/M1-DESIGN.md`.**

- [ ] **Step 1: full build** — `go build github.com/example/agent-native-microkernel/... github.com/example/agent-native-os/plugins/... github.com/example/agent-native-os/cli/...` → exit 0.

- [ ] **Step 2: all Go tests** — `go test ./plugins/... ./plugins/_template ./cli/... && (cd kernel && go test ./...)` → all `ok`.

- [ ] **Step 3: kernel regression untouched** — `cd kernel && ./scripts/build.sh >/dev/null && python3 tests/integration/m05_qualification.py 2>&1 | tail -2` → `M0.5 ADVERSARIAL QUALIFICATION: PASSED`.

- [ ] **Step 4: architecture checks** — `bash scripts/check-arch.sh` → `CONTRACT CHECK: PASSED (16 contracts, ...)`, `COMPOSITION FITNESS: PASSED (5 manifests)`, `ARCHITECTURE FITNESS: PASSED`, `ARCH CHECKS OK`.

- [ ] **Step 5: smoke** — `bash scripts/smoke.sh` → `M1.3 AGENT SMOKE: OK` and `M1 SMOKE: PASSED`.

- [ ] **Step 6: G1 kernel purity**

```bash
git diff --stat 3bdbe77 HEAD -- kernel/
git diff --name-only 3bdbe77 HEAD -- kernel/internal kernel/cmd kernel/sdk
```

Expected: **both empty**. If either shows output, stop and report.

- [ ] **Step 7: open the PR** — `chatgpt/m1-3-agent-adapter` → `main`, title **M1.3 — Agent Adapter (mock provider)**, body: the 8 tasks, the verbatim acceptance output from Steps 3–6, and any deviations. Do not create a docs commit.

---

## Self-Review

**Spec coverage (`docs/M1-DESIGN.md` §13 M1.3 = "agent-adapter：只 mock provider，agent.run streaming 打通 + AgentRun 持久化"):**
- provider-neutral `agent.run@1` streaming command → Tasks 1, 5. `agent.frame` frames stream live over the kernel stream channel (verified end-to-end by the smoke).
- mock provider only, with the §8.3 fault knobs (fail / cancel / partial) → Task 3.
- `AgentRun` persisted, survives restart → Tasks 2, 7 (smoke restart).
- `raw_session_ref` via `blob.put` (§3, §5.1) → Tasks 5, 6 (`service_authority` for the autonomous post-stream call).
- mock writes into the worktree so M1.4 has a diff → Task 3 (`MockWriteFile`), verified in the smoke.
- get / query / cancel → Task 6.
- G1 → Task 8.
- Deferred correctly: real CLI provider + runtime discovery (M1.8); interrupting the actual in-flight goroutine on `agent.run.cancel` (M1.8 — M1.3 cancel only flips the record); the workflow driving `agent.run` inside the pipeline (M1.6).

**Do-not-touch:** `docs/M1-DESIGN.md` is not edited or committed by any task. §13 status is the reviewer's post-merge step.

**Placeholder scan:** schemas keep `agent_run` / `agent_runs` as bare objects/arrays — deliberate. `mock_*` request fields are real, documented test knobs, not placeholders. No `TBD` / "handle errors" / "similar to Task N".

**Type consistency:** `AgentRun` (fields `ID/WorkContextID/WorkspacePath/Prompt/Provider/HarnessNativeID/Status/RawSessionRef/ProviderMetadata/FrameCount/StartedAt/EndedAt`), status constants `StatusRunning..StatusTimeout`, `Store` + `Load`/`RecordStarted`/`RecordCompleted`/`RecordCancelled`/`GetByID`/`QueryByContext`, `ErrNotFound`/`ErrAlreadyTerminal` — Task 2, used in 5–6. `Frame`/`RunSpec`/`RunResult`/`Provider`/`MockProvider` — Task 3, used in 4–6. `Transcript`/`runProvider` — Task 4, used in 5. `runDeps`/`runOnce`/`agentRunHandler`/`getHandler`/`queryHandler`/`cancelHandler` — Tasks 5–6. `invokeStream` — Task 7, used in `main.go` of the CLI. Contract/service/authority names (`default-agent-harness` / `agent-runs-main`) consistent across manifest, bindings, fence-lease test fixtures, and smoke.
