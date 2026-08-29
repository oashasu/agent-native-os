# M1.4 — Artifact Service + Tool Runner — Implementation Plan

**Execution:** work task-by-task, in order. Each task: write the failing test first, run it and watch it fail for the right reason, write the minimal implementation, run it green, then (for behavioural changes) briefly invert the production change to confirm the test goes red and restore it. Commit at the end of each task with the message given.

**Goal:** Land two atomic plugins the workflow needs to turn agent output into evidence:
- **`org.vibe.artifact`** — captures the git diff of a worktree as a content-addressed Artifact + a numeric summary.
- **`org.vibe.tool.runner`** — runs a deterministic command (structured argv, no shell string) in a worktree, captures exit/stdout/stderr, and produces a `ToolRun` with a reproducible fingerprint. `outcome` is `PASS` (exit 0) or `FAIL`.

**Architecture:** Two stateful plugins, each behind three contracts, each with its own authority and its own append-only JSONL log + projection (same pattern as `work-registry` / `workspace` / `agent-harness`). Both consume `blob.put@1` to store bytes (diff text; stdout/stderr) — the call is made **inside the request handler** (the operations are synchronous), so no `service_authority` is needed. Git and command execution shell out to real binaries; helpers isolate that.

**Tech Stack:** Go standard library + `kernel/sdk/go/{protocol,pluginhost,fencing}` (via `go.work`, **no new external Go dependency**), the `git` binary, newline-delimited JSON over a Unix socket, Python 3 + `jsonschema`.

**Spec:** `docs/M1-DESIGN.md` — §3 (chain: `artifact.collect_diff` → `Artifact{kind=diff}`; `tool.run(label=build/test)` with structured argv), §5.1 (both consume `blob.put`), §5.2 (`org.vibe.artifact` / `org.vibe.tool.runner` rows), §6 (`Artifact` and `ToolRun` shapes; fingerprint = `hash(command + env_allowlist值 + workspace.base_commit)`), §10 (build vs test command split; structured argv, `cwd`, `env_allowlist`), §13 milestone M1.4.

**Base:** branch `chatgpt/m1-4-artifact-toolrunner` from `main` at `e44bd07`. Present: everything through M1.3 — `plugins/foundation/{blob,event-journal}`, `plugins/{work-registry,workspace,agent-harness}`, `cli/vibe`, `scripts/{build,dev-run,smoke,check-arch}.sh` + `scripts/smoke-{workspace,agent}.sh`, `config/m1-{policy,bindings}.json` with 16 contracts and 5 plugins wired, `architecture-tests/check_composition.py`. `restart_kernel` in `scripts/smoke.sh` already probes query-readiness.

## Global Constraints

- **G1 Kernel Purity:** no task modifies `kernel/` source. G1 check: `git diff --name-only e44bd07 HEAD -- kernel/internal kernel/cmd kernel/sdk` must be empty.
- **Do NOT touch `docs/M1-DESIGN.md`.** Do not stage, edit, or commit it. The milestone-status line is updated separately by the reviewer after merge.
- **No new external Go modules.**
- **Module paths:** kernel `github.com/example/agent-native-microkernel`; plugins `github.com/example/agent-native-os/plugins`; CLI `github.com/example/agent-native-os/cli`.
- **Manifest rule:** export/consume `contract` field == `<capability>@<major>` exactly. Stateful exports need `mode: "stateful"`, `service`, `authority`; manifest needs `runtime.data_namespace`.
- **Contract rule:** `contracts/<dotted.name>/v<major>/schema.json`, register in `contracts/catalog.json`, then `python3 scripts/check-contracts.py --root contracts`.
- **Git identity in tests/scripts:** environment may have no global git config — every `git commit` in a test or script passes `-c user.email=test@example.com -c user.name=test` inline. A worktree cannot be added to a repo with no commits.
- **Commit trailer** — every commit message ends with exactly:
  ```
  Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
  Plan: docs/superpowers/plans/2026-08-29-m1-4-artifact-toolrunner.md
  ```
- **Commit identity:** author `ada <oashasu@gmail.com>` (the GitHub connector may substitute its authenticated identity — that is a known connector limit, not a deviation to fix).
- **Number discipline:** any count in an expected-output string is whatever the plan's own commands produce; if a literal here differs, trust the commands, adjust the assertion, note it.

---

## File Structure

New:

- `contracts/artifact.collect-diff/v1/schema.json`, `contracts/artifact.get/v1/schema.json`, `contracts/artifact.query/v1/schema.json`
- `contracts/tool.run/v1/schema.json`, `contracts/tool.run.get/v1/schema.json`, `contracts/tool.run.query/v1/schema.json`
- `plugins/artifact/gitdiff.go`, `plugins/artifact/gitdiff_test.go`
- `plugins/artifact/store.go`, `plugins/artifact/store_test.go`
- `plugins/artifact/handlers.go`, `plugins/artifact/handlers_test.go`
- `plugins/artifact/main.go`
- `plugins/manifests/artifact.manifest.json`
- `plugins/tool-runner/runner.go`, `plugins/tool-runner/runner_test.go`
- `plugins/tool-runner/store.go`, `plugins/tool-runner/store_test.go`
- `plugins/tool-runner/handlers.go`, `plugins/tool-runner/handlers_test.go`
- `plugins/tool-runner/main.go`
- `plugins/manifests/tool-runner.manifest.json`
- `scripts/smoke-artifact.sh` — the M1.4 smoke fragment

Modified:

- `contracts/catalog.json` — +6 entries
- `config/m1-policy.json` — `local-cli` grants + `org.vibe.artifact` / `org.vibe.tool.runner` grants (each `capabilities: ["blob.put@1"]`, **no** `service_authority`)
- `config/m1-bindings.json` — bindings for the 6 stateful capabilities
- `cli/vibe/main.go` — `artifact` and `tool` subcommands
- `scripts/smoke.sh` — `source scripts/smoke-artifact.sh` (after the agent fragment)

Responsibilities: `gitdiff.go` / `runner.go` shell out and parse; `store.go` owns persistence; `handlers.go` wires envelope ↔ (helper + blob + store); `main.go` only wires.

---

## Task 1: artifact contracts

**Files:** three schemas; modify `contracts/catalog.json`.

`Artifact` shape (documented once; schemas keep `artifact` a bare object):

```
Artifact = {
  id, work_context_id,
  kind:     "diff" | "command_output",
  blob_uri, summary:  { files_changed, insertions, deletions, files: [string] },
  created_at
}
```

- [ ] **Step 1: `contracts/artifact.collect-diff/v1/schema.json`** — `contract` `"artifact.collect_diff@1"`, `kind` `"command"`, request:

```json
  "request": {
    "type": "object",
    "additionalProperties": false,
    "properties": {
      "work_context_id": { "type": "string" },
      "workspace_path": { "type": "string" },
      "base_ref": { "type": "string" }
    },
    "required": ["work_context_id", "workspace_path"]
  }
```

response `{ "artifact": { "type": "object" } }` required.

- [ ] **Step 2: `contracts/artifact.get/v1/schema.json`** — `kind` `"query"`, request `{ "artifact_id": string }` required, response `{ "artifact": object }` required.

- [ ] **Step 3: `contracts/artifact.query/v1/schema.json`** — `kind` `"query"`, request `{ "work_context_id": string }` required, response `{ "artifacts": { "type": "array" } }` required.

- [ ] **Step 4: catalog + check**

Add to `contracts/catalog.json`:

```json
  "artifact.collect_diff@1": "artifact.collect-diff/v1/schema.json",
  "artifact.get@1": "artifact.get/v1/schema.json",
  "artifact.query@1": "artifact.query/v1/schema.json"
```

Run: `python3 scripts/check-contracts.py --root contracts` → `CONTRACT CHECK: PASSED` (count = catalog entries now: 16 + 3 = 19).

- [ ] **Step 5: commit**

```
build(m1.4): artifact.collect_diff/get/query contracts
```

---

## Task 2: `gitdiff.go` helper

**Files:** `plugins/artifact/gitdiff.go`, `plugins/artifact/gitdiff_test.go`.

**Interfaces:**

```go
type DiffSummary struct {
	FilesChanged int      `json:"files_changed"`
	Insertions   int      `json:"insertions"`
	Deletions    int      `json:"deletions"`
	Files        []string `json:"files"`
}

// collectDiff runs `git diff <base>` for the patch text and `git diff --numstat <base>`
// for the summary, and appends untracked file names (from `ls-files --others
// --exclude-standard`) to Files with a "(untracked)" suffix. base defaults to "HEAD".
func collectDiff(workspacePath, base string) (patch string, summary DiffSummary, err error)
```

- [ ] **Step 1: write the failing test**

`plugins/artifact/gitdiff_test.go`:

```go
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitRepoWithChange(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	os.WriteFile(filepath.Join(dir, "Calc.java"), []byte("class Calc { int add(int a,int b){return a+b;} }\n"), 0o644)
	run("add", "-A")
	run("commit", "-q", "-m", "init")
	// tracked change + a new untracked file
	os.WriteFile(filepath.Join(dir, "Calc.java"), []byte("class Calc { int add(int a,int b){ if(a<0) throw new RuntimeException(); return a+b;} }\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "CalcTest.java"), []byte("// new test\n"), 0o644)
	return dir
}

func TestCollectDiffCapturesTrackedAndUntracked(t *testing.T) {
	ws := gitRepoWithChange(t)
	patch, sum, err := collectDiff(ws, "")
	if err != nil {
		t.Fatalf("collectDiff: %v", err)
	}
	if !strings.Contains(patch, "Calc.java") || !strings.Contains(patch, "RuntimeException") {
		t.Fatalf("patch missing the tracked change:\n%s", patch)
	}
	if sum.FilesChanged < 1 || sum.Insertions < 1 {
		t.Fatalf("summary: %+v", sum)
	}
	joined := strings.Join(sum.Files, ",")
	if !strings.Contains(joined, "Calc.java") || !strings.Contains(joined, "CalcTest.java") {
		t.Fatalf("files list missing an entry: %v", sum.Files)
	}
}

func TestCollectDiffCleanTree(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"commit", "-q", "--allow-empty", "-m", "e"}} {
		c := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", args, out)
		}
	}
	_, sum, err := collectDiff(dir, "")
	if err != nil || sum.FilesChanged != 0 || len(sum.Files) != 0 {
		t.Fatalf("clean tree summary: %+v err=%v", sum, err)
	}
}
```

- [ ] **Step 2: run to verify failure.**

- [ ] **Step 3: implement `gitdiff.go`.**

- A `git(dir, args...) (string, error)` helper (mirror `plugins/workspace/gitworktree.go`'s).
- `collectDiff`: `if base == "" { base = "HEAD" }`; `patch, _ = git(ws, "--no-pager", "diff", base)`; parse `git(ws, "--no-pager", "diff", "--numstat", base)` — each line `<ins>\t<del>\t<path>` (a `-` in ins/del means binary → count as 0); build `summary`; then `git(ws, "ls-files", "--others", "--exclude-standard")` — each non-empty line is an untracked file → append `<name> (untracked)` to `summary.Files` and `summary.FilesChanged++`. Return.

- [ ] **Step 4: run green.**

- [ ] **Step 5: mutation check** — make `collectDiff` skip the `ls-files --others` step. Run `go test ./plugins/artifact/... -run TestCollectDiffCapturesTrackedAndUntracked` → FAIL (`CalcTest.java` missing). Restore.

- [ ] **Step 6: commit**

```
feat(m1.4): git diff collector
```

---

## Task 3: artifact store + projection

**Files:** `plugins/artifact/store.go`, `plugins/artifact/store_test.go`.

**Interfaces:** `type Store`, `Load(dir string) (*Store, error)` (opens `dir/artifact-log.jsonl`), `Record(a Artifact) error`, `GetByID(id string) (Artifact, bool)`, `QueryByContext(wcID string) []Artifact`. Types: `Artifact`, `DiffSummary` (reuse from `gitdiff.go` — same package), `ErrNotFound`. Same JSONL-log + single `apply` reducer + fsync-per-append + torn-last-line tolerance as `plugins/workspace/store.go`.

- [ ] **Step 1: write the failing test**

`plugins/artifact/store_test.go`:

```go
package main

import "testing"

func TestRecordAndReadBack(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := Artifact{ID: "a1", WorkContextID: "wc-1", Kind: "diff", BlobURI: "blob://sha256/x", Summary: DiffSummary{FilesChanged: 2, Insertions: 5}, CreatedAt: "t0"}
	if err := s.Record(a); err != nil {
		t.Fatal(err)
	}
	got, ok := s.GetByID("a1")
	if !ok || got.Kind != "diff" || got.Summary.FilesChanged != 2 {
		t.Fatalf("get: %+v ok=%v", got, ok)
	}
	if q := s.QueryByContext("wc-1"); len(q) != 1 || q[0].ID != "a1" {
		t.Fatalf("query: %+v", q)
	}
}

func TestArtifactProjectionRebuilds(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_ = s.Record(Artifact{ID: "a1", WorkContextID: "wc-1", Kind: "diff", CreatedAt: "t1"})
	_ = s.Record(Artifact{ID: "a2", WorkContextID: "wc-1", Kind: "command_output", CreatedAt: "t2"})
	re, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if q := re.QueryByContext("wc-1"); len(q) != 2 || q[0].ID != "a1" || q[1].ID != "a2" {
		t.Fatalf("rebuilt query: %+v", q)
	}
}
```

- [ ] **Step 2: run to verify failure.**

- [ ] **Step 3: implement `store.go`** mirroring `plugins/workspace/store.go` — one op `"artifact.recorded"`; `Store { mu; path; seq; byID map[string]*Artifact; byCtx map[string][]string }`; clone on read.

- [ ] **Step 4: run green.**

- [ ] **Step 5: mutation check** — in `apply`, skip appending the id to `byCtx`. Run `go test ./plugins/artifact/... -run TestArtifactProjectionRebuilds` → FAIL. Restore.

- [ ] **Step 6: commit**

```
feat(m1.4): artifact store — JSONL log + projection
```

---

## Task 4: artifact handlers + plugin wiring

**Files:** `plugins/artifact/handlers.go`, `plugins/artifact/handlers_test.go`, `plugins/artifact/main.go`, `plugins/manifests/artifact.manifest.json`.

**Interfaces:** `collectDiffHandler(s *Store, blobPut func([]byte) (string, error)) pluginhost.Handler` — fenced stateful command; `getHandler(s *Store)` / `queryHandler(s *Store)` — stateful queries.

- [ ] **Step 1: write the failing test**

`plugins/artifact/handlers_test.go`:

```go
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/agent-native-microkernel/sdk/go/protocol"
)

func fencedEnv(t *testing.T, dir string) protocol.Envelope {
	t.Helper()
	fenceRoot := filepath.Join(dir, ".fences")
	_ = os.MkdirAll(fenceRoot, 0o755)
	t.Setenv("VIBE_DATA_DIR", dir)
	t.Setenv("VIBE_RUNTIME_ID", "rt-test")
	t.Setenv("VIBE_FENCE_ROOT", fenceRoot)
	lease := map[string]any{"service": "default-artifact", "authority": "artifact-main", "runtime_id": "rt-test", "epoch": 1}
	b, _ := json.Marshal(lease)
	_ = os.WriteFile(filepath.Join(fenceRoot, "default-artifact--artifact-main.json"), b, 0o644)
	return protocol.Envelope{Protocol: 1, MessageID: "m", Kind: protocol.KindCommand, Service: "default-artifact", Authority: "artifact-main", FencingEpoch: 1}
}

func TestCollectDiffHandlerRecordsArtifact(t *testing.T) {
	ws := gitRepoWithChange(t) // from gitdiff_test.go, same package
	dir := t.TempDir()
	s, _ := Load(dir)
	var putN int
	put := func(b []byte) (string, error) { putN++; return "blob://sha256/deadbeef", nil }
	env := fencedEnv(t, dir)
	env.Payload = protocol.NewPayload(map[string]string{"work_context_id": "wc-1", "workspace_path": ws})

	out, perr := collectDiffHandler(s, put)(env)
	if perr != nil {
		t.Fatalf("collect: %+v", perr)
	}
	var r struct{ Artifact Artifact `json:"artifact"` }
	b, _ := json.Marshal(out)
	_ = json.Unmarshal(b, &r)
	if r.Artifact.Kind != "diff" || r.Artifact.BlobURI != "blob://sha256/deadbeef" || r.Artifact.Summary.FilesChanged < 1 {
		t.Fatalf("artifact: %+v", r.Artifact)
	}
	if putN != 1 {
		t.Fatalf("blob.put called %d times, want 1", putN)
	}
	if _, ok := s.GetByID(r.Artifact.ID); !ok {
		t.Fatal("artifact not recorded")
	}
}

func TestCollectDiffHandlerRejectsMissingFields(t *testing.T) {
	s, _ := Load(t.TempDir())
	env := fencedEnv(t, t.TempDir())
	env.Payload = protocol.NewPayload(map[string]string{"work_context_id": "wc-1"})
	_, perr := collectDiffHandler(s, func([]byte) (string, error) { return "", nil })(env)
	if perr == nil || perr.Code != "INVALID" {
		t.Fatalf("want INVALID, got %+v", perr)
	}
}
```

- [ ] **Step 2: run to verify failure.**

- [ ] **Step 3: implement `handlers.go`.**
- `collectDiffHandler`: parse `{work_context_id, workspace_path, base_ref}`; missing required → `INVALID`; `collectDiff(workspace_path, base_ref)` → error → `GIT_ERROR`; inside `fencing.WithWriteFence(e, ...)`: `uri, err := blobPut([]byte(patch))` → on error `IO`; build `Artifact{ID: protocol.NewID("art"), WorkContextID, Kind:"diff", BlobURI:uri, Summary:sum, CreatedAt: time.Now().UTC().RFC3339Nano}`; `s.Record(a)`. Return `{artifact: a}`.
- `getHandler`: `{artifact_id}` required → `GetByID` → `NOT_FOUND` → `{artifact}`.
- `queryHandler`: `{work_context_id}` required → `QueryByContext` → `{artifacts: [...]}` (always array).

- [ ] **Step 4: write `main.go`**

```go
package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"time"

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
		panic("artifact load: " + err.Error())
	}
	h := pluginhost.New("org.vibe.artifact", "1.0.0", "")
	blobPut := func(payload []byte) (string, error) {
		resp, cerr := h.Command("blob.put", 1, map[string]string{"content_base64": base64.StdEncoding.EncodeToString(payload)}, 30*time.Second)
		if cerr != nil {
			return "", cerr
		}
		var r struct{ URI string `json:"uri"` }
		_ = json.Unmarshal(resp.Payload, &r)
		return r.URI, nil
	}
	h.HandleContextCommand("artifact.collect_diff", 1, func(rc *pluginhost.RequestContext, e protocol.Envelope) (any, *protocol.Error) {
		// blob.put here rides the request delegation (rc.Command), not h.Command;
		// wrap so the fenced write uses this request's envelope.
		return collectDiffHandler(s, func(b []byte) (string, error) {
			resp, cerr := rc.Command("blob.put", 1, map[string]string{"content_base64": base64.StdEncoding.EncodeToString(b)}, 30*time.Second)
			if cerr != nil {
				return "", cerr
			}
			var r struct{ URI string `json:"uri"` }
			_ = json.Unmarshal(resp.Payload, &r)
			return r.URI, nil
		})(e)
	})
	h.HandleQuery("artifact.get", 1, getHandler(s))
	h.HandleQuery("artifact.query", 1, queryHandler(s))
	_ = blobPut
	_ = h.Serve()
}
```

(The `collect_diff` handler uses `rc.Command` for `blob.put` — the call happens synchronously inside the request, so it rides the request-scoped delegation and needs no `service_authority`. `blobPut` via `h.Command` is left defined but unused; delete it if `go vet` complains, or just keep the `rc.Command` closure inline.)

- [ ] **Step 5: `plugins/manifests/artifact.manifest.json`**

```json
{
  "manifest_version": 1,
  "plugin": { "id": "org.vibe.artifact", "version": "1.0.0" },
  "runtime": {
    "protocol": "vibe-plugin/1",
    "executable": "../bin/artifact",
    "isolation": "process",
    "data_namespace": "state-authority/artifact-main"
  },
  "exports": [
    { "capability": "artifact.collect_diff", "major": 1, "contract": "artifact.collect_diff@1", "mode": "stateful", "service": "default-artifact", "authority": "artifact-main", "priority": 100 },
    { "capability": "artifact.get", "major": 1, "contract": "artifact.get@1", "mode": "stateful", "service": "default-artifact", "authority": "artifact-main", "priority": 100 },
    { "capability": "artifact.query", "major": 1, "contract": "artifact.query@1", "mode": "stateful", "service": "default-artifact", "authority": "artifact-main", "priority": 100 }
  ],
  "consumes": { "required": [ { "capability": "blob.put", "major": 1, "contract": "blob.put@1" } ] },
  "permissions": ["exec:git", "fs:read"],
  "restart": { "mode": "on_failure", "max_attempts": 2, "cooldown_ms": 200 },
  "resources": { "memory_mb": 128, "cpu_weight": 20 }
}
```

- [ ] **Step 6: build + composition** — `bash scripts/build.sh` → `built plugin: artifact`; `python3 architecture-tests/check_composition.py` → PASSED (manifest count now 6).

- [ ] **Step 7: commit**

```
feat(m1.4): artifact.collect_diff + get/query + plugin wiring
```

---

## Task 5: tool contracts

**Files:** three schemas; modify `contracts/catalog.json`.

`ToolRun` shape:

```
ToolRun = {
  id, work_context_id, workspace_path, label,
  command: [string], cwd, env_allowlist: [string], timeout_ms,
  exit_code, outcome: "PASS" | "FAIL",
  stdout_uri, stderr_uri, fingerprint,
  started_at, ended_at
}
```

- [ ] **Step 1: `contracts/tool.run/v1/schema.json`** — `kind` `"command"`, request:

```json
  "request": {
    "type": "object",
    "additionalProperties": false,
    "properties": {
      "work_context_id": { "type": "string" },
      "workspace_path": { "type": "string" },
      "label": { "type": "string" },
      "command": { "type": "array", "items": { "type": "string" }, "minItems": 1 },
      "env_allowlist": { "type": "array", "items": { "type": "string" } },
      "timeout_ms": { "type": "integer" }
    },
    "required": ["work_context_id", "workspace_path", "label", "command"]
  }
```

response `{ "tool_run": { "type": "object" } }` required.

- [ ] **Step 2: `contracts/tool.run.get/v1/schema.json`** — `kind` `"query"`, request `{ "tool_run_id": string }` required, response `{ "tool_run": object }` required.

- [ ] **Step 3: `contracts/tool.run.query/v1/schema.json`** — `kind` `"query"`, request `{ "work_context_id": string }` required, response `{ "tool_runs": { "type": "array" } }` required.

- [ ] **Step 4: catalog + check** — add `tool.run@1` / `tool.run.get@1` / `tool.run.query@1` → paths `tool.run/v1/...`, `tool.run.get/v1/...`, `tool.run.query/v1/...`. `python3 scripts/check-contracts.py --root contracts` → PASSED (count 19 + 3 = 22).

- [ ] **Step 5: commit**

```
build(m1.4): tool.run/get/query contracts
```

---

## Task 6: `runner.go` helper

**Files:** `plugins/tool-runner/runner.go`, `plugins/tool-runner/runner_test.go`.

**Interfaces:**

```go
type CmdResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
	TimedOut bool
}

// runCommand runs argv with cwd=workspacePath, passing only PATH+HOME plus the
// named env vars in envAllowlist (read from the plugin process environment).
// timeoutMS <= 0 -> 10 minutes. A non-zero exit is NOT an error; only a failure
// to start the process is.
func runCommand(ctx context.Context, workspacePath string, argv, envAllowlist []string, timeoutMS int) (CmdResult, error)

// fingerprint is a stable hex digest of the command + the values of the allowlisted
// env vars (sorted) + the workspace's current commit sha (best-effort via git).
func fingerprint(argv, envAllowlist []string, workspacePath string) string
```

- [ ] **Step 1: write the failing test**

`plugins/tool-runner/runner_test.go`:

```go
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
```

- [ ] **Step 2: run to verify failure.**

- [ ] **Step 3: implement `runner.go`.**
- `runCommand`: `if timeoutMS <= 0 { timeoutMS = 600000 }`; `cctx, cancel := context.WithTimeout(ctx, ...)`; `cmd := exec.CommandContext(cctx, argv[0], argv[1:]...)`; `cmd.Dir = workspacePath`; `cmd.Env = []string{"PATH="+os.Getenv("PATH"), "HOME="+os.Getenv("HOME")}` then for each name in `envAllowlist` if `os.Getenv(name) != ""` append `name+"="+value`; capture `cmd.Stdout`/`cmd.Stderr` into `bytes.Buffer`s; `err := cmd.Run()`; `r.ExitCode = cmd.ProcessState.ExitCode()` (—1 if killed); `r.TimedOut = cctx.Err() == context.DeadlineExceeded`; if the process failed to *start* (`exec.Error` / `*os.PathError`) return that as the error; otherwise return `r, nil`.
- `fingerprint`: `sha256` of `json(argv)` + `"\x00"` + `json(sortedEnvValues)` + `"\x00"` + `commitOf(workspacePath)`, hex; `commitOf` = `git -C <ws> rev-parse HEAD` (empty string on error).

- [ ] **Step 4: run green.**

- [ ] **Step 5: mutation check** — in `runCommand`, build `cmd.Env` from the full `os.Environ()` instead of the allowlist. Run `go test ./plugins/tool-runner/... -run TestRunCommandOnlyPassesAllowlistedEnv` → FAIL (`sensitive` leaks). Restore. Then in `fingerprint`, ignore `argv` (hash only env+commit). Run `-run TestFingerprintStableAndSensitive` → FAIL (`f1 == f3`). Restore.

- [ ] **Step 6: commit**

```
feat(m1.4): deterministic command runner + fingerprint
```

---

## Task 7: tool store + projection

**Files:** `plugins/tool-runner/store.go`, `plugins/tool-runner/store_test.go`.

Same shape as Task 3 but for `ToolRun`. One op `"tool.run.recorded"`. `Store { mu; path; seq; byID map[string]*ToolRun; byCtx map[string][]string }`. `Record(tr ToolRun) error`, `GetByID`, `QueryByContext`. Mirror `plugins/artifact/store.go`.

- [ ] **Step 1: failing test** (`plugins/tool-runner/store_test.go`) — record a `ToolRun{ID:"t1", WorkContextID:"wc-1", Label:"build", Outcome:"PASS", ExitCode:0, Fingerprint:"abc"}`, read it back by id and by context; a second Load rebuilds the projection with two runs in order.

- [ ] **Step 2: run to verify failure.**

- [ ] **Step 3: implement `store.go`.**

- [ ] **Step 4: run green.**

- [ ] **Step 5: mutation check** — in `apply`, drop the `byCtx` append; the projection-rebuild test fails on `QueryByContext` length. Restore.

- [ ] **Step 6: commit**

```
feat(m1.4): tool-runner store — JSONL log + projection
```

---

## Task 8: tool.run handler + plugin wiring

**Files:** `plugins/tool-runner/handlers.go`, `plugins/tool-runner/handlers_test.go`, `plugins/tool-runner/main.go`, `plugins/manifests/tool-runner.manifest.json`.

**Interfaces:** `toolRunHandler(s *Store, blobPut func([]byte) (string, error)) pluginhost.ContextHandler`, `getHandler(s *Store)`, `queryHandler(s *Store)`.

- [ ] **Step 1: write the failing test**

`plugins/tool-runner/handlers_test.go`:

```go
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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
	lease := map[string]any{"service": "default-tool-runner", "authority": "toolruns-main", "runtime_id": "rt-test", "epoch": 1}
	b, _ := json.Marshal(lease)
	_ = os.WriteFile(filepath.Join(fenceRoot, "default-tool-runner--toolruns-main.json"), b, 0o644)
	return protocol.Envelope{Protocol: 1, MessageID: "m", Kind: protocol.KindCommand, Service: "default-tool-runner", Authority: "toolruns-main", FencingEpoch: 1}
}

func run(t *testing.T, s *Store, dir string, label string, argv []string) ToolRun {
	t.Helper()
	env := fencedEnv(t, dir)
	env.Payload = protocol.NewPayload(map[string]any{
		"work_context_id": "wc-1", "workspace_path": t.TempDir(), "label": label, "command": argv, "timeout_ms": 5000,
	})
	blobN := 0
	h := toolRunHandler(s, func([]byte) (string, error) { blobN++; return "blob://sha256/x", nil })
	out, perr := h(&pluginhost.RequestContext{}, env)
	if perr != nil {
		t.Fatalf("%s: %+v", label, perr)
	}
	if blobN != 2 {
		t.Fatalf("%s: blob.put called %d times, want 2 (stdout+stderr)", label, blobN)
	}
	var r struct{ ToolRun ToolRun `json:"tool_run"` }
	b, _ := json.Marshal(out)
	_ = json.Unmarshal(b, &r)
	return r.ToolRun
}

func TestToolRunPassAndFail(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	pass := run(t, s, dir, "build", []string{"sh", "-c", "echo building; exit 0"})
	if pass.Outcome != "PASS" || pass.ExitCode != 0 || pass.Fingerprint == "" || pass.StdoutURI == "" {
		t.Fatalf("pass run: %+v", pass)
	}
	fail := run(t, s, dir, "test", []string{"sh", "-c", "echo testing; exit 1"})
	if fail.Outcome != "FAIL" || fail.ExitCode != 1 {
		t.Fatalf("fail run: %+v", fail)
	}
	if _, ok := s.GetByID(pass.ID); !ok {
		t.Fatal("pass run not recorded")
	}
}

func TestToolRunHandlerRejectsEmptyCommand(t *testing.T) {
	s, _ := Load(t.TempDir())
	h := toolRunHandler(s, func([]byte) (string, error) { return "", nil })
	env := fencedEnv(t, t.TempDir())
	env.Payload = protocol.NewPayload(map[string]any{"work_context_id": "wc-1", "workspace_path": "/tmp", "label": "x", "command": []string{}})
	_, perr := h(&pluginhost.RequestContext{}, env)
	if perr == nil || perr.Code != "INVALID" {
		t.Fatalf("want INVALID, got %+v", perr)
	}
}
```

Note: `toolRunHandler` must accept a `context.Context` from `rc` in production but the test passes `&pluginhost.RequestContext{}`; guard with `ctx := context.Background(); if rc != nil { ctx = rc.Context() }` OR keep the runnable core in a `runTool(ctx, deps, req)` function the handler calls, and unit-test `runTool` directly with `context.Background()`. Prefer the latter (mirrors M1.3's `runOnce`): `toolRunHandler` validates + fences + calls `runTool`.

- [ ] **Step 2: run to verify failure.**

- [ ] **Step 3: implement `handlers.go`.** `runTool(ctx, s, blobPut, req)`:
- `res, err := runCommand(ctx, req.WorkspacePath, req.Command, req.EnvAllowlist, req.TimeoutMS)` → start error → `IO`.
- `stdoutURI, _ := blobPut(res.Stdout)`, `stderrURI, _ := blobPut(res.Stderr)` (always both, even if empty).
- `outcome := "PASS"; if res.ExitCode != 0 || res.TimedOut { outcome = "FAIL" }`.
- `tr := ToolRun{ID: protocol.NewID("trun"), WorkContextID, WorkspacePath, Label, Command: req.Command, Cwd: req.WorkspacePath, EnvAllowlist: req.EnvAllowlist, TimeoutMS: effectiveTimeout, ExitCode: res.ExitCode, Outcome: outcome, StdoutURI, StderrURI, Fingerprint: fingerprint(req.Command, req.EnvAllowlist, req.WorkspacePath), StartedAt, EndedAt}`.
- `s.Record(tr)`; return `tr`.
`toolRunHandler`: parse+validate (`work_context_id`, `workspace_path`, `label` non-empty, `len(command) >= 1`) → `INVALID`; `ctx` from rc; `fencing.WithWriteFence(e, func() error { tr, err = runTool(ctx, s, blobPut, req); return err })`; return `{tool_run: tr}`.

- [ ] **Step 4: write `main.go`** — mirror `plugins/artifact/main.go`: `blobPut` closure over `rc.Command("blob.put", ...)`. Register `tool.run` (context command), `tool.run.get` / `tool.run.query` (queries).

- [ ] **Step 5: `plugins/manifests/tool-runner.manifest.json`** — same shape as artifact's but `id` `org.vibe.tool.runner`, executable `../bin/tool-runner`, `data_namespace` `state-authority/toolruns-main`, exports `tool.run@1` / `tool.run.get@1` / `tool.run.query@1` (service `default-tool-runner`, authority `toolruns-main`), `consumes` `blob.put@1`, `permissions` `["exec:command", "fs:write"]`, `resources` `{ "memory_mb": 512, "cpu_weight": 40 }` (mvn is heavy).

- [ ] **Step 6: build + composition** — `bash scripts/build.sh` → `built plugin: tool-runner`; `check_composition.py` → PASSED (manifest count now 7).

- [ ] **Step 7: commit**

```
feat(m1.4): tool.run + get/query + plugin wiring
```

---

## Task 9: policy, bindings, CLI, smoke

**Files:** modify `config/m1-policy.json`, `config/m1-bindings.json`, `cli/vibe/main.go`, `scripts/smoke.sh`; create `scripts/smoke-artifact.sh`.

- [ ] **Step 1: policy + bindings**

`config/m1-policy.json` — add to `grants.local-cli.capabilities`:

```json
  "artifact.collect_diff@1", "artifact.get@1", "artifact.query@1",
  "tool.run@1", "tool.run.get@1", "tool.run.query@1"
```

Add grants:

```json
  "org.vibe.artifact": { "capabilities": ["blob.put@1"] },
  "org.vibe.tool.runner": { "capabilities": ["blob.put@1"] }
```

(No `service_authority` — both plugins call `blob.put` synchronously inside the request, riding the caller's delegation. `local-cli` already has `blob.put@1` directly, so the delegated child call is authorised.)

`config/m1-bindings.json` — add bindings for `artifact.collect_diff` / `artifact.get` / `artifact.query` (service `default-artifact`, authority `artifact-main`) and `tool.run` / `tool.run.get` / `tool.run.query` (service `default-tool-runner`, authority `toolruns-main`).

- [ ] **Step 2: `vibe artifact` + `vibe tool` subcommands** in `cli/vibe/main.go`

- `vibe artifact collect-diff <work-context-id> -workspace <path> [-base-ref <ref>]` → `artifact.collect_diff@1`; print `artifact <id>  files_changed <n>  +<ins> -<del>  blob <uri>`.
- `vibe artifact show <artifact-id>` → `artifact.get@1`; print key fields, `-json` for raw.
- `vibe tool run <work-context-id> -workspace <path> -label <l> [-timeout-ms N] -- <cmd> [args...]` → everything after `--` is `command`; `tool.run@1`; print `tool_run <id>  outcome <PASS|FAIL>  exit <code>  fp <fingerprint-first-12>  stdout <uri>`.
- `vibe tool show <tool-run-id>` → `tool.run.get@1`; print key fields, `-json` for raw.

Parse `--` yourself: find the first `"--"` in the subcommand args, everything after is the command argv.

- [ ] **Step 3: write `scripts/smoke-artifact.sh`**

```bash
#!/usr/bin/env bash
# M1.4 smoke fragment: after the mock agent changed a worktree file (M1.3 fragment
# leaves $WT populated is NOT guaranteed here — this fragment builds its own), collect
# the diff, run a passing and a failing tool command, verify evidence + blob refs +
# fingerprint, and survive a kernel restart.
set -euo pipefail
V=".bin/vibe -socket $SOCK -identity local-cli -token $TOKEN"
RAW=".bin/vibe-raw -socket $SOCK -identity local-cli -token $TOKEN"

SRC="$DATA/artsrc"
mkdir -p "$SRC"
git -C "$SRC" -c init.defaultBranch=main init -q
printf 'class Calc { int add(int a,int b){return a+b;} }\n' > "$SRC/Calc.java"
git -C "$SRC" add -A
git -C "$SRC" -c user.email=s@t -c user.name=s -c commit.gpgsign=false commit -q -m init

WC_ID="$($V task create -title "art smoke" -goal g -repo "$SRC" | sed -n 's/.*wc \([^ ]*\).*/\1/p')"
WT="$($V workspace allocate "$WC_ID" -repo "$SRC" | sed -n 's/.*path \([^ ]*\).*/\1/p')"
$V agent run "$WC_ID" -workspace "$WT" -prompt "harden add" -steps 2 -write-file Calc.java -write-content '// hardened by agent
' >/dev/null
sleep 0.5

# --- collect diff ---
diff_out="$($V artifact collect-diff "$WC_ID" -workspace "$WT")"
ART_ID="$(echo "$diff_out" | sed -n 's/^artifact \([^ ]*\).*/\1/p')"
echo "$diff_out" | grep -q 'files_changed [1-9]' || { echo "FAIL: no diff detected: $diff_out"; exit 1; }
DIFF_BLOB="$(echo "$diff_out" | sed -n 's/.*blob \([^ ]*\).*/\1/p')"
$RAW -cap blob.get -kind query -service default-blob -authority blob-main -payload "{\"uri\":\"$DIFF_BLOB\"}" | grep -q content_base64 \
  || { echo "FAIL: diff blob not resolvable"; exit 1; }

# --- passing build ---
build_out="$($V tool run "$WC_ID" -workspace "$WT" -label build -- sh -c 'echo compiling; exit 0')"
echo "$build_out" | grep -q 'outcome PASS' || { echo "FAIL: build: $build_out"; exit 1; }
echo "$build_out" | grep -qE 'fp [0-9a-f]{12}' || { echo "FAIL: no fingerprint: $build_out"; exit 1; }
BUILD_TR="$(echo "$build_out" | sed -n 's/^tool_run \([^ ]*\).*/\1/p')"
SOUT="$($V tool show "$BUILD_TR" -json | sed -n 's/.*"stdout_uri":"\([^"]*\)".*/\1/p')"
$RAW -cap blob.get -kind query -service default-blob -authority blob-main -payload "{\"uri\":\"$SOUT\"}" \
  | python3 -c 'import sys,json,base64; d=json.load(sys.stdin); print(base64.b64decode(d["content_base64"]).decode())' | grep -q compiling \
  || { echo "FAIL: build stdout blob missing 'compiling'"; exit 1; }

# --- failing test ---
test_out="$($V tool run "$WC_ID" -workspace "$WT" -label test -- sh -c 'echo running; exit 1')"
echo "$test_out" | grep -q 'outcome FAIL' || { echo "FAIL: failing test not FAIL: $test_out"; exit 1; }
echo "$test_out" | grep -q 'exit 1' || { echo "FAIL: exit code not 1: $test_out"; exit 1; }

restart_kernel
for _ in $(seq 1 50); do
  $V artifact show "$ART_ID" 2>/dev/null | grep -q 'diff' && break
  sleep 0.1
done
$V artifact show "$ART_ID" 2>/dev/null | grep -q 'diff' || { echo "FAIL: artifact lost on restart"; exit 1; }
$V tool show "$BUILD_TR" 2>/dev/null | grep -q 'PASS' || { echo "FAIL: tool run lost on restart"; exit 1; }

echo "M1.4 ARTIFACT+TOOL SMOKE: OK"
```

- [ ] **Step 4: wire into `scripts/smoke.sh`** — add `source scripts/smoke-artifact.sh` after `source scripts/smoke-agent.sh`, before the final `echo "M1 SMOKE: PASSED"`.

- [ ] **Step 5: run the smoke** — `bash scripts/smoke.sh` → `M1.4 ARTIFACT+TOOL SMOKE: OK` then `M1 SMOKE: PASSED`. Run it 5 times to confirm no flake. Common failures: missing binding, `local-cli` missing a grant, the `--` command parsing in the CLI, `blob.put` not authorised for the plugin (needs `blob.put@1` in its grant).

- [ ] **Step 6: commit**

```
feat(m1.4): M1 policy/bindings for artifact+tool-runner, vibe subcommands, smoke
```

---

## Task 10: acceptance gate + PR

**Files:** none — **do not touch `docs/M1-DESIGN.md`.**

- [ ] **Step 1: full build** — `go build github.com/example/agent-native-microkernel/... github.com/example/agent-native-os/plugins/... github.com/example/agent-native-os/cli/...` → exit 0.

- [ ] **Step 2: all Go tests** — `go test ./plugins/... ./plugins/_template ./cli/... && (cd kernel && go test ./...)` → all `ok`.

- [ ] **Step 3: kernel regression** — `cd kernel && ./scripts/build.sh >/dev/null && python3 tests/integration/m05_qualification.py 2>&1 | tail -2` → `M0.5 ADVERSARIAL QUALIFICATION: PASSED`.

- [ ] **Step 4: architecture checks** — `bash scripts/check-arch.sh` → `CONTRACT CHECK: PASSED (22 contracts, ...)`, `COMPOSITION FITNESS: PASSED (7 manifests)`, `ARCHITECTURE FITNESS: PASSED`, `ARCH CHECKS OK`.

- [ ] **Step 5: smoke (×5)** — `for i in 1 2 3 4 5; do bash scripts/smoke.sh 2>&1 | grep -E "ARTIFACT\+TOOL|M1 SMOKE|FAIL"; done` → every run ends `M1.4 ARTIFACT+TOOL SMOKE: OK` then `M1 SMOKE: PASSED`, no `FAIL`.

- [ ] **Step 6: G1 + design purity**

```bash
git diff --name-only e44bd07 HEAD -- kernel/internal kernel/cmd kernel/sdk
git diff --name-only e44bd07 HEAD -- docs/M1-DESIGN.md
```

Both must be empty.

- [ ] **Step 7: open the PR** — `chatgpt/m1-4-artifact-toolrunner` → `main`, title **M1.4 — Artifact Service + Tool Runner**, body: the 10 tasks, the verbatim acceptance output from Steps 3–6, and any deviations. No docs commit.

---

## Self-Review

**Spec coverage (`docs/M1-DESIGN.md` §13 M1.4 = "artifact-service（collect_diff）+ tool-runner（结构化 argv + 指纹 + blob 输出）"):**
- `artifact.collect_diff` → `Artifact{kind=diff, blob_uri, summary}` (§3, §6) → Tasks 1–4. Untracked files captured too.
- `tool.run` with **structured argv** (no shell string), `cwd`, `env_allowlist`, reproducible `fingerprint` (§6, §10) → Tasks 5–8. `outcome` = PASS(exit 0) / FAIL.
- stdout/stderr → `blob.put` (§6, §5.1) → Task 8. Synchronous, rides delegation, no `service_authority`.
- build vs test as two `tool.run` calls with different `label` + argv (§10) → smoke Task 9.
- both survive restart → Task 9 smoke.
- G1 + do-not-touch-design → Task 10.
- Deferred correctly: immutable JUnit snapshot / structured test parsing (M2 — §11 NON-GOAL for M1); the workflow driving collect_diff + tool.run in the pipeline and attaching the results as `EvidenceRef` via `work.attach_evidence` (M1.6); real `mvn` commands (M1.9 — smoke here uses `sh -c` so it is network- and Maven-free).

**Do-not-touch:** `docs/M1-DESIGN.md` is not edited or committed by any task.

**Placeholder scan:** schemas keep `artifact` / `tool_run` / arrays as bare types — deliberate. No `TBD` / "handle errors" / "similar to Task N".

**Type consistency:** `DiffSummary` defined in `plugins/artifact/gitdiff.go`, reused by `plugins/artifact/store.go` + handlers (same package `main`). `Artifact` (Task 3) → Task 4. `collectDiff(ws, base) (string, DiffSummary, error)` — Task 2, used in Task 4. `CmdResult` / `runCommand(ctx, ws, argv, envAllow, timeoutMS)` / `fingerprint(argv, envAllow, ws)` — Task 6, used in Task 8. `ToolRun` (Task 7) → Task 8. `runTool` / `toolRunHandler` / `getHandler` / `queryHandler` per plugin. Service/authority names: `default-artifact`/`artifact-main`, `default-tool-runner`/`toolruns-main` — consistent across manifests, bindings, fence-lease test fixtures, smoke. `blob.put` grant present in both plugin policy grants.
