# M1.2 — Workspace Manager (git worktree) — Implementation Plan

**Execution:** work task-by-task, in order. Each task: write the failing test first, run it and watch it fail for the right reason, write the minimal implementation, run it green, then (for behavioural changes) briefly invert the production change to confirm the test goes red and restore it. Commit at the end of each task with the message given.

**Goal:** Land `org.vibe.workspace` — the plugin that allocates an isolated git worktree + branch for a WorkContext, tracks it, and releases it (preserving the worktree on disk in M1). Plus `vibe workspace` CLI subcommands and smoke coverage.

**Architecture:** A stateful plugin behind three contracts. Persistence is the same append-only JSONL log + in-memory projection pattern used by `work-registry` and `event-journal` — no database. Git operations shell out to the `git` binary through a small `gitworktree.go` helper (no external Go module). Worktrees live under the plugin's own data dir (`$VIBE_DATA_DIR/worktrees/<workspace-id>`). The WorkContext ↔ Workspace link is `WorkspaceRef.work_context_id`, queried via `workspace.get`; nothing is written back onto the WorkContext.

**Tech Stack:** Go standard library + `kernel/sdk/go/{protocol,pluginhost,fencing}` (resolved via the `go.work` workspace — **no new external Go dependencies**), the `git` CLI, newline-delimited JSON over a Unix socket, Python 3 + `jsonschema` for contract checks.

**Spec:** `docs/M1-DESIGN.md` — §3 (chain: `workspace.allocate` / `workspace.release(policy=preserve)`), §5.2 (`org.vibe.workspace` row), §6 (`WorkspaceRef` shape), §10 (`workspace.release` only after seal; M1 keeps the worktree), §13 milestone M1.2.

**Base:** branch `chatgpt/m1-2-workspace-manager` from `main` at `c72965e`. Present: everything M1.0 + M1.1 delivered (`go.work` with `./kernel ./plugins ./cli`, `plugins/foundation/{blob,event-journal}`, `plugins/work-registry`, `cli/vibe`, `scripts/{build,dev-run,smoke,check-arch}.sh`, `config/m1-{policy,bindings}.json` with 9 contracts wired, `architecture-tests/check_composition.py`).

## Global Constraints

- **G1 Kernel Purity:** no task modifies `kernel/` source. M1 code only consumes `kernel/sdk/go/...`. G1 check: `git diff --name-only c72965e HEAD -- kernel/internal kernel/cmd kernel/sdk` must be empty. If a task seems to need a kernel change, stop and report.
- **No new external Go modules.** No `go get`, no `v0.0.0` pseudo-version requires. The workspace resolves the kernel SDK with no `replace`.
- **Module paths:** kernel `github.com/example/agent-native-microkernel`; plugins `github.com/example/agent-native-os/plugins`; CLI `github.com/example/agent-native-os/cli`.
- **`go` directive** in any new `go.mod`: `go 1.19` (none needed this milestone). Leave `go.work` at `go 1.23`.
- **Manifest rule:** an export/consume `contract` field MUST equal `<capability>@<major>` exactly. Stateful exports need `mode: "stateful"`, `service`, `authority`; the manifest needs `runtime.data_namespace`.
- **Contract rule:** `contracts/<dotted.name>/v<major>/schema.json`, `contract` = identity, `kind` ∈ command|query|event, `version` starting `"<major>."`, Draft 2020-12 `request`/`response`. Register in `contracts/catalog.json`, then `python3 scripts/check-contracts.py --root contracts`.
- **Git identity in tests:** the build environment may have no global git config. Every `git commit` in a test or script must pass identity inline: `git -C <dir> -c user.email=test@example.com -c user.name=test commit ...`. `git init` a repo, stage a file, commit — a worktree cannot be added to a repo with no commits.
- **Commit trailer** — every commit message ends with exactly:
  ```
  Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
  Plan: docs/superpowers/plans/2026-08-29-m1-2-workspace-manager.md
  ```
- **Commit identity:** author `ada <oashasu@gmail.com>`.
- **Number discipline:** any count in an expected-output string is whatever the plan's own commands produce; if a literal here differs, trust the commands, adjust the assertion, note it in the final report.

---

## File Structure

New:

- `contracts/workspace.allocate/v1/schema.json`, `contracts/workspace.release/v1/schema.json`, `contracts/workspace.get/v1/schema.json`
- `plugins/workspace/gitworktree.go`, `plugins/workspace/gitworktree_test.go` — git CLI helper
- `plugins/workspace/store.go`, `plugins/workspace/store_test.go` — `WorkspaceRef` + JSONL log + projection
- `plugins/workspace/handlers.go`, `plugins/workspace/handlers_test.go` — the three capability handlers
- `plugins/workspace/main.go` — wiring
- `plugins/manifests/workspace.manifest.json`
- `scripts/smoke-workspace.sh` — the M1.2 smoke fragment (invoked from `scripts/smoke.sh`)

Modified:

- `contracts/catalog.json` — +3 entries
- `config/m1-policy.json` — `local-cli` grants for the 3 workspace capabilities
- `config/m1-bindings.json` — bindings for the 3 stateful capabilities
- `cli/vibe/main.go` — `workspace` subcommand (`allocate` / `show` / `release`)
- `scripts/smoke.sh` — call `scripts/smoke-workspace.sh` before the final `PASSED`

Responsibilities: `gitworktree.go` knows only about running `git` and parsing its output; `store.go` owns persistence + projection and nothing about the protocol; `handlers.go` maps envelopes ↔ (git + store) and owns validation; `main.go` only wires.

---

## Task 1: workspace contracts

**Files:** three schemas; modify `contracts/catalog.json`.

`WorkspaceRef` shape (documented once; schemas keep `workspace` as a bare object):

```
WorkspaceRef = {
  id, work_context_id, repo, path, branch, base_commit,
  status:         "ALLOCATED" | "RELEASED",
  release_policy: "" | "preserve" | "delete",
  allocated_at, released_at    (released_at is "" until released)
}
```

- [ ] **Step 1: `contracts/workspace.allocate/v1/schema.json`**

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "contract": "workspace.allocate@1",
  "version": "1.0.0",
  "kind": "command",
  "compatibility": "backward-within-major",
  "request": {
    "type": "object",
    "additionalProperties": false,
    "properties": {
      "work_context_id": { "type": "string" },
      "repo": { "type": "string" },
      "base_ref": { "type": "string" }
    },
    "required": ["work_context_id", "repo"]
  },
  "response": {
    "type": "object",
    "additionalProperties": true,
    "properties": { "workspace": { "type": "object" } },
    "required": ["workspace"]
  }
}
```

- [ ] **Step 2: `contracts/workspace.release/v1/schema.json`** — `kind` `"command"`, request:

```json
  "request": {
    "type": "object",
    "additionalProperties": false,
    "properties": {
      "workspace_id": { "type": "string" },
      "policy": { "type": "string", "enum": ["preserve", "delete"] }
    },
    "required": ["workspace_id", "policy"]
  }
```

response: `{ "workspace": object }` required.

- [ ] **Step 3: `contracts/workspace.get/v1/schema.json`** — `kind` `"query"`, request:

```json
  "request": {
    "type": "object",
    "additionalProperties": false,
    "properties": {
      "workspace_id": { "type": "string" },
      "work_context_id": { "type": "string" }
    },
    "required": []
  }
```

response: `{ "workspace": object }` required. (Handler requires exactly one selector.)

- [ ] **Step 4: catalog + check**

Add to `contracts/catalog.json`:

```json
  "workspace.allocate@1": "workspace.allocate/v1/schema.json",
  "workspace.release@1": "workspace.release/v1/schema.json",
  "workspace.get@1": "workspace.get/v1/schema.json"
```

Run: `python3 scripts/check-contracts.py --root contracts` → `CONTRACT CHECK: PASSED` (count = entries now in catalog.json: 9 + 3 = 12).

- [ ] **Step 5: commit**

```
build(m1.2): workspace.allocate/release/get contracts
```

---

## Task 2: `gitworktree.go` helper

**Files:** `plugins/workspace/gitworktree.go`, `plugins/workspace/gitworktree_test.go`.

**Interfaces:**
- Produces: `ensureRepo(repo string) error`, `resolveCommit(repo, ref string) (string, error)` (ref `""` → `HEAD`; returns 40-hex), `addWorktree(repo, path, branch, commit string) error`, `removeWorktree(repo, path string) error`, `deleteBranch(repo, branch string) error` (best-effort). All shell `git` via `exec.Command`; on failure return an error containing the combined stdout+stderr.

- [ ] **Step 1: write the failing test**

`plugins/workspace/gitworktree_test.go`:

```go
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// scratchRepo makes a git repo with one commit and returns its path + the commit sha.
func scratchRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", dir,
			"-c", "user.email=test@example.com", "-c", "user.name=test",
			"-c", "commit.gpgsign=false"}, args...)...)
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-q", "-m", "init")
	return dir, run("rev-parse", "HEAD")
}

func TestResolveCommitAndAddRemoveWorktree(t *testing.T) {
	repo, head := scratchRepo(t)

	got, err := resolveCommit(repo, "")
	if err != nil || got != head {
		t.Fatalf("resolveCommit HEAD = %q err=%v, want %q", got, err, head)
	}

	wt := filepath.Join(t.TempDir(), "wt")
	if err := addWorktree(repo, wt, "aeos/ws-test", head); err != nil {
		t.Fatalf("addWorktree: %v", err)
	}
	// The worktree exists, is on the new branch, at the base commit.
	br := gitOut(t, wt, "rev-parse", "--abbrev-ref", "HEAD")
	if br != "aeos/ws-test" {
		t.Fatalf("worktree branch = %q", br)
	}
	if sha := gitOut(t, wt, "rev-parse", "HEAD"); sha != head {
		t.Fatalf("worktree HEAD = %q, want %q", sha, head)
	}

	if err := removeWorktree(repo, wt); err != nil {
		t.Fatalf("removeWorktree: %v", err)
	}
	if _, statErr := os.Stat(wt); !os.IsNotExist(statErr) {
		t.Fatalf("worktree dir still present after remove")
	}
}

func TestEnsureRepoRejectsNonRepo(t *testing.T) {
	if err := ensureRepo(t.TempDir()); err == nil {
		t.Fatal("ensureRepo should fail on a non-git directory")
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
```

- [ ] **Step 2: run to verify failure** — `undefined: resolveCommit` etc.

- [ ] **Step 3: implement `gitworktree.go`**

```go
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func git(repo string, args ...string) (string, error) {
	c := exec.Command("git", append([]string{"-C", repo}, args...)...)
	out, err := c.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func ensureRepo(repo string) error {
	_, err := git(repo, "rev-parse", "--git-dir")
	return err
}

func resolveCommit(repo, ref string) (string, error) {
	if ref == "" {
		ref = "HEAD"
	}
	return git(repo, "rev-parse", "--verify", ref+"^{commit}")
}

func addWorktree(repo, path, branch, commit string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	_, err := git(repo, "worktree", "add", "-b", branch, path, commit)
	return err
}

func removeWorktree(repo, path string) error {
	if _, err := git(repo, "worktree", "remove", "--force", path); err != nil {
		return err
	}
	_, _ = git(repo, "worktree", "prune")
	return nil
}

func deleteBranch(repo, branch string) error {
	_, err := git(repo, "branch", "-D", branch)
	return err
}
```

- [ ] **Step 4: run the tests green.**

- [ ] **Step 5: mutation check** — make `resolveCommit` ignore `ref` and always use `"HEAD"` — the existing test still passes (it asks for HEAD). Instead: make `addWorktree` drop the `-b <branch>` args. Run `go test ./plugins/workspace/... -run TestResolveCommitAndAddRemoveWorktree` → FAIL (branch is not `aeos/ws-test`). Restore.

- [ ] **Step 6: commit**

```
feat(m1.2): git worktree helper
```

---

## Task 3: workspace store + projection

**Files:** `plugins/workspace/store.go`, `plugins/workspace/store_test.go`.

**Interfaces:**
- Produces: `type Store`, `Load(dir string) (*Store, error)` (opens `dir/workspace-log.jsonl`, replays), `RecordAllocated(ref WorkspaceRef) error`, `RecordReleased(id, policy string) (WorkspaceRef, error)`, `GetByID(id string) (WorkspaceRef, bool)`, `GetActiveByContext(wcID string) (WorkspaceRef, bool)` (most-recent `ALLOCATED`). Types: `WorkspaceRef`, `Status` (`StatusAllocated="ALLOCATED"`, `StatusReleased="RELEASED"`). Same JSONL-log + single `apply` reducer + fsync-per-append + torn-last-line tolerance as `plugins/work-registry/store.go` — read that file and mirror its structure.

- [ ] **Step 1: write the failing test**

`plugins/workspace/store_test.go`:

```go
package main

import "testing"

func TestRecordAllocatedThenGet(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	ref := WorkspaceRef{
		ID: "ws-1", WorkContextID: "wc-1", Repo: "/tmp/r", Path: "/tmp/wt", Branch: "aeos/ws-1",
		BaseCommit: "abc", Status: StatusAllocated, AllocatedAt: "t0",
	}
	if err := s.RecordAllocated(ref); err != nil {
		t.Fatalf("record: %v", err)
	}
	got, ok := s.GetByID("ws-1")
	if !ok || got.Status != StatusAllocated || got.Branch != "aeos/ws-1" {
		t.Fatalf("get: %+v ok=%v", got, ok)
	}
	active, ok := s.GetActiveByContext("wc-1")
	if !ok || active.ID != "ws-1" {
		t.Fatalf("active by context: %+v ok=%v", active, ok)
	}
}

func TestReleaseUpdatesStatusAndSurvivesReload(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_ = s.RecordAllocated(WorkspaceRef{ID: "ws-1", WorkContextID: "wc-1", Status: StatusAllocated})
	out, err := s.RecordReleased("ws-1", "preserve")
	if err != nil || out.Status != StatusReleased || out.ReleasePolicy != "preserve" {
		t.Fatalf("release: %+v err=%v", out, err)
	}
	if _, ok := s.GetActiveByContext("wc-1"); ok {
		t.Fatalf("released workspace must not be 'active' for its context")
	}
	reloaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := reloaded.GetByID("ws-1")
	if got.Status != StatusReleased || got.ReleasePolicy != "preserve" {
		t.Fatalf("release did not survive reload: %+v", got)
	}
}

func TestReleaseUnknownIsNotFound(t *testing.T) {
	s, _ := Load(t.TempDir())
	if _, err := s.RecordReleased("nope", "preserve"); err == nil {
		t.Fatal("releasing an unknown workspace must error")
	}
}
```

- [ ] **Step 2: run to verify failure.**

- [ ] **Step 3: implement `store.go`** mirroring `plugins/work-registry/store.go`:

- `WorkspaceRef` struct with json tags matching Task 1's shape (`id`, `work_context_id`, `repo`, `path`, `branch`, `base_commit`, `status`, `release_policy`, `allocated_at`, `released_at`).
- `logRecord { Seq int64; TS string; Op string; Data json.RawMessage }`, ops `"workspace.allocated"`, `"workspace.released"`.
- `Store { mu sync.Mutex; path string; seq int64; byID map[string]*WorkspaceRef }`.
- `Load`: `MkdirAll`; read `workspace-log.jsonl`; split on `\n`; for each non-empty line `json.Unmarshal` into `logRecord` — if it fails on the last line **and** the file has no trailing newline, `break` (torn write); otherwise return an error; `apply(rec)`; track max seq.
- `apply`: `"workspace.allocated"` → unmarshal a `WorkspaceRef`, store a copy in `byID`; `"workspace.released"` → unmarshal `{workspace_id, policy, released_at}`, look up in `byID` (→ `ErrNotFound` if absent), set `Status = StatusReleased`, `ReleasePolicy = policy`, `ReleasedAt = released_at`.
- `append(op, data)`: `seq++`; marshal `logRecord`; `OpenFile(O_APPEND|O_CREATE|O_WRONLY)`; write line + `\n`; `f.Sync()`; propagate write/sync error even if `Close` succeeds; then `apply`; then `s.seq = rec.Seq`.
- `RecordAllocated(ref)`: lock; `append("workspace.allocated", ref)`.
- `RecordReleased(id, policy)`: lock; if `id` not in `byID` → `ErrNotFound`; `append("workspace.released", {workspace_id:id, policy:policy, released_at: time.Now().UTC().RFC3339Nano})`; return a copy of the updated ref.
- `GetByID`: lock; return copy + ok.
- `GetActiveByContext(wcID)`: lock; scan `byID` for `WorkContextID == wcID && Status == StatusAllocated`; if several, return the one with the lexicographically-greatest `AllocatedAt` (RFC3339Nano sorts chronologically); return copy + ok.
- `ErrNotFound = errors.New("workspace not found")`.
- Return copies from every read (`func cloneRef(*WorkspaceRef) WorkspaceRef`), never internal pointers.

- [ ] **Step 4: run the tests green.**

- [ ] **Step 5: mutation check** — in `apply` for `"workspace.released"`, skip setting `Status` (leave it `ALLOCATED`). Run `go test ./plugins/workspace/... -run TestReleaseUpdatesStatusAndSurvivesReload` → FAIL. Restore.

- [ ] **Step 6: commit**

```
feat(m1.2): workspace store — JSONL log + projection
```

---

## Task 4: `workspace.allocate` handler

**Files:** `plugins/workspace/handlers.go`, `plugins/workspace/handlers_test.go`.

**Interfaces:** `allocateHandler(s *Store) pluginhost.Handler` — a fenced stateful command (service `default-workspace`, authority `workspace-main`). Branch name `aeos/ws-<last 8 of workspace id>`. Worktree path `filepath.Join(os.Getenv("VIBE_DATA_DIR"), "worktrees", workspaceID)`.

- [ ] **Step 1: write the failing test**

`plugins/workspace/handlers_test.go`:

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
	lease := map[string]any{"service": "default-workspace", "authority": "workspace-main", "runtime_id": "rt-test", "epoch": 1}
	b, _ := json.Marshal(lease)
	_ = os.WriteFile(filepath.Join(fenceRoot, "default-workspace--workspace-main.json"), b, 0o644)
	return protocol.Envelope{
		Protocol: 1, MessageID: "m", Kind: protocol.KindCommand,
		Service: "default-workspace", Authority: "workspace-main", FencingEpoch: 1,
	}
}

func TestAllocateCreatesWorktreeOnNewBranch(t *testing.T) {
	repo, head := scratchRepo(t)
	dir := t.TempDir()
	s, _ := Load(dir)
	env := fencedEnv(t, dir)
	env.Payload = protocol.NewPayload(map[string]string{"work_context_id": "wc-42", "repo": repo})

	out, perr := allocateHandler(s)(env)
	if perr != nil {
		t.Fatalf("allocate: %+v", perr)
	}
	var r struct{ Workspace WorkspaceRef `json:"workspace"` }
	b, _ := json.Marshal(out)
	_ = json.Unmarshal(b, &r)
	ws := r.Workspace
	if ws.WorkContextID != "wc-42" || ws.BaseCommit != head || ws.Status != StatusAllocated {
		t.Fatalf("workspace: %+v", ws)
	}
	if br := gitOut(t, ws.Path, "rev-parse", "--abbrev-ref", "HEAD"); br != ws.Branch {
		t.Fatalf("worktree on %q, ref says %q", br, ws.Branch)
	}
	if _, ok := s.GetByID(ws.ID); !ok {
		t.Fatalf("allocate did not record the workspace")
	}
}

func TestAllocateRejectsNonRepo(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	env := fencedEnv(t, dir)
	env.Payload = protocol.NewPayload(map[string]string{"work_context_id": "wc-1", "repo": t.TempDir()})
	_, perr := allocateHandler(s)(env)
	if perr == nil || perr.Code != "GIT_ERROR" {
		t.Fatalf("want GIT_ERROR for a non-repo, got %+v", perr)
	}
}

func TestAllocateRejectsMissingFields(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	env := fencedEnv(t, dir)
	env.Payload = protocol.NewPayload(map[string]string{"work_context_id": "wc-1"}) // no repo
	_, perr := allocateHandler(s)(env)
	if perr == nil || perr.Code != "INVALID" {
		t.Fatalf("want INVALID, got %+v", perr)
	}
}
```

- [ ] **Step 2: run to verify failure.**

- [ ] **Step 3: implement `allocateHandler` in `handlers.go`**

- `allocateRequest { WorkContextID, Repo, BaseRef string }`. If `WorkContextID == "" || Repo == ""` → `INVALID`.
- `ensureRepo(Repo)` fails → `{Code:"GIT_ERROR", Message: err}`.
- `resolveCommit(Repo, BaseRef)` fails → `GIT_ERROR`.
- `id := protocol.NewID("ws")`; `branch := "aeos/ws-" + last8(id)`; `path := filepath.Join(os.Getenv("VIBE_DATA_DIR"), "worktrees", id)`.
- Inside `fencing.WithWriteFence(e, func() error { ... })`: `addWorktree(Repo, path, branch, commit)`; if it fails return that error; build `ref := WorkspaceRef{ID:id, WorkContextID, Repo, Path:path, Branch:branch, BaseCommit:commit, Status:StatusAllocated, AllocatedAt: time.Now().UTC().RFC3339Nano}`; `s.RecordAllocated(ref)`.
- Map a fence/store/git error → `{Code:"GIT_ERROR"}` if it mentions `git`, else `{Code:"IO", Retryable:true}`. Simplest: if `addWorktree` failed → `GIT_ERROR`; if `RecordAllocated` failed → `IO`. Keep the two error sites separate so the code is distinguishable — on `addWorktree` failure also best-effort `removeWorktree` to avoid a dangling worktree.
- Return `map[string]any{"workspace": ref}`.
- Helper `last8(s string) string` → last 8 runes, or the whole string if shorter.

- [ ] **Step 4: run green.**

- [ ] **Step 5: mutation check** — drop the `ensureRepo` call. Run `go test ./plugins/workspace/... -run TestAllocateRejectsNonRepo` → the git `worktree add` will still fail so it may still error, but with a different code/path — assert the test still gets `GIT_ERROR`; if not, the `ensureRepo` check is load-bearing for the message and the test should pin `perr.Code == "GIT_ERROR"` regardless. Restore.

- [ ] **Step 6: commit**

```
feat(m1.2): workspace.allocate handler
```

---

## Task 5: `workspace.release` + `workspace.get` handlers + plugin wiring

**Files:** modify `plugins/workspace/handlers.go`, `plugins/workspace/handlers_test.go`; create `plugins/workspace/main.go`, `plugins/manifests/workspace.manifest.json`.

- [ ] **Step 1: write the failing tests** (append to `handlers_test.go`):

```go
func allocate(t *testing.T, s *Store, dir, repo, wc string) WorkspaceRef {
	t.Helper()
	env := fencedEnv(t, dir)
	env.Payload = protocol.NewPayload(map[string]string{"work_context_id": wc, "repo": repo})
	out, perr := allocateHandler(s)(env)
	if perr != nil {
		t.Fatal(perr)
	}
	var r struct{ Workspace WorkspaceRef `json:"workspace"` }
	b, _ := json.Marshal(out)
	_ = json.Unmarshal(b, &r)
	return r.Workspace
}

func TestReleasePreserveKeepsWorktreeOnDisk(t *testing.T) {
	repo, _ := scratchRepo(t)
	dir := t.TempDir()
	s, _ := Load(dir)
	ws := allocate(t, s, dir, repo, "wc-1")

	env := fencedEnv(t, dir)
	env.Payload = protocol.NewPayload(map[string]string{"workspace_id": ws.ID, "policy": "preserve"})
	out, perr := releaseHandler(s)(env)
	if perr != nil {
		t.Fatalf("release: %+v", perr)
	}
	var r struct{ Workspace WorkspaceRef `json:"workspace"` }
	b, _ := json.Marshal(out)
	_ = json.Unmarshal(b, &r)
	if r.Workspace.Status != StatusReleased || r.Workspace.ReleasePolicy != "preserve" {
		t.Fatalf("released ref: %+v", r.Workspace)
	}
	if _, err := os.Stat(ws.Path); err != nil {
		t.Fatalf("preserve policy must keep the worktree dir: %v", err)
	}
}

func TestReleaseDeleteRemovesWorktree(t *testing.T) {
	repo, _ := scratchRepo(t)
	dir := t.TempDir()
	s, _ := Load(dir)
	ws := allocate(t, s, dir, repo, "wc-1")

	env := fencedEnv(t, dir)
	env.Payload = protocol.NewPayload(map[string]string{"workspace_id": ws.ID, "policy": "delete"})
	if _, perr := releaseHandler(s)(env); perr != nil {
		t.Fatalf("release delete: %+v", perr)
	}
	if _, err := os.Stat(ws.Path); !os.IsNotExist(err) {
		t.Fatalf("delete policy must remove the worktree dir")
	}
}

func TestGetBySelector(t *testing.T) {
	repo, _ := scratchRepo(t)
	dir := t.TempDir()
	s, _ := Load(dir)
	ws := allocate(t, s, dir, repo, "wc-1")

	for _, sel := range []map[string]string{{"workspace_id": ws.ID}, {"work_context_id": "wc-1"}} {
		out, perr := getHandler(s)(protocol.Envelope{Payload: protocol.NewPayload(sel)})
		if perr != nil {
			t.Fatalf("get %v: %+v", sel, perr)
		}
		var r struct{ Workspace WorkspaceRef `json:"workspace"` }
		b, _ := json.Marshal(out)
		_ = json.Unmarshal(b, &r)
		if r.Workspace.ID != ws.ID {
			t.Fatalf("get %v returned %s", sel, r.Workspace.ID)
		}
	}
	_, perr := getHandler(s)(protocol.Envelope{Payload: protocol.NewPayload(map[string]string{})})
	if perr == nil || perr.Code != "INVALID" {
		t.Fatalf("no selector must be INVALID, got %+v", perr)
	}
}
```

- [ ] **Step 2: run to verify failure.**

- [ ] **Step 3: implement `releaseHandler` + `getHandler`**

`releaseHandler`:
- `releaseRequest { WorkspaceID, Policy string }`. `WorkspaceID == "" || (Policy != "preserve" && Policy != "delete")` → `INVALID`.
- `s.GetByID(WorkspaceID)` → not found → `NOT_FOUND`.
- If already `StatusReleased`: if same policy → return it (idempotent); different policy → `CONFLICT`.
- Inside `fencing.WithWriteFence`: if `Policy == "delete"` → `removeWorktree(ref.Repo, ref.Path)` (on error → `GIT_ERROR`); best-effort `deleteBranch(ref.Repo, ref.Branch)` (ignore error). Then `s.RecordReleased(WorkspaceID, Policy)`.
- Return `{workspace: updated}`.

`getHandler`:
- `getRequest { WorkspaceID, WorkContextID string }`. `(WorkspaceID == "") == (WorkContextID == "")` → `INVALID` (exactly one).
- `WorkspaceID != ""` → `s.GetByID`; else `s.GetActiveByContext`.
- not found → `NOT_FOUND`.
- Return `{workspace: ref}`.

- [ ] **Step 4: write `main.go`**

```go
package main

import (
	"os"

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
		panic("workspace load: " + err.Error())
	}
	h := pluginhost.New("org.vibe.workspace", "1.0.0", "")
	h.HandleContextCommand("workspace.allocate", 1, wrap(allocateHandler(s)))
	h.HandleContextCommand("workspace.release", 1, wrap(releaseHandler(s)))
	h.HandleQuery("workspace.get", 1, getHandler(s))
	_ = h.Serve()
}

func wrap(fn pluginhost.Handler) pluginhost.ContextHandler {
	return func(_ *pluginhost.RequestContext, e protocol.Envelope) (any, *protocol.Error) { return fn(e) }
}
```

- [ ] **Step 5: `plugins/manifests/workspace.manifest.json`**

```json
{
  "manifest_version": 1,
  "plugin": { "id": "org.vibe.workspace", "version": "1.0.0" },
  "runtime": {
    "protocol": "vibe-plugin/1",
    "executable": "../bin/workspace",
    "isolation": "process",
    "data_namespace": "state-authority/workspace-main"
  },
  "exports": [
    { "capability": "workspace.allocate", "major": 1, "contract": "workspace.allocate@1", "mode": "stateful", "service": "default-workspace", "authority": "workspace-main", "priority": 100 },
    { "capability": "workspace.release", "major": 1, "contract": "workspace.release@1", "mode": "stateful", "service": "default-workspace", "authority": "workspace-main", "priority": 100 },
    { "capability": "workspace.get", "major": 1, "contract": "workspace.get@1", "mode": "stateful", "service": "default-workspace", "authority": "workspace-main", "priority": 100 }
  ],
  "permissions": ["exec:git", "fs:write"],
  "restart": { "mode": "on_failure", "max_attempts": 2, "cooldown_ms": 100 },
  "resources": { "memory_mb": 128, "cpu_weight": 20 }
}
```

(`permissions` is declarative in M1 — the reference kernel does not enforce a host-resource sandbox yet, per `kernel/docs/04`. Declaring it is honest and future-proof.)

- [ ] **Step 6: build + composition check**

Run: `cd <repo-root> && bash scripts/build.sh` → expect `built plugin: workspace` among the others, `BUILD OK`.
Run: `python3 architecture-tests/check_composition.py` → `COMPOSITION FITNESS: PASSED` (manifest count now 4).

- [ ] **Step 7: commit**

```
feat(m1.2): workspace.release + workspace.get + plugin wiring
```

---

## Task 6: policy, bindings, `vibe workspace`, smoke

**Files:** modify `config/m1-policy.json`, `config/m1-bindings.json`, `cli/vibe/main.go`, `scripts/smoke.sh`; create `scripts/smoke-workspace.sh`.

- [ ] **Step 1: policy + bindings**

`config/m1-policy.json` — add to `grants.local-cli.capabilities`:

```json
  "workspace.allocate@1",
  "workspace.release@1",
  "workspace.get@1"
```

Add `grants."org.vibe.workspace"` with `"capabilities": []` (it exports, consumes nothing).

`config/m1-bindings.json` — add:

```json
{ "capability": "workspace.allocate", "major": 1, "service": "default-workspace", "authority": "workspace-main" },
{ "capability": "workspace.release", "major": 1, "service": "default-workspace", "authority": "workspace-main" },
{ "capability": "workspace.get", "major": 1, "service": "default-workspace", "authority": "workspace-main" }
```

- [ ] **Step 2: `vibe workspace` subcommand**

In `cli/vibe/main.go`, add a `workspace` command alongside `task`:

- `vibe workspace allocate <work-context-id> -repo <path> [-base-ref <ref>]` → `workspace.allocate@1` command; print `workspace <id>  branch <branch>  path <path>  base <base_commit>`.
- `vibe workspace show <workspace-id>` OR `vibe workspace show -work-context <wc-id>` → `workspace.get@1` query; print `id / status / branch / path / base_commit`, or `-json` for the raw payload.
- `vibe workspace release <workspace-id> -policy preserve|delete` → `workspace.release@1` command; print `status <status>  policy <policy>`.

Keep it thin — reuse the existing `invoke` helper and the global `-socket/-identity/-token` flags exactly as the `task` command does.

- [ ] **Step 3: write `scripts/smoke-workspace.sh`**

```bash
#!/usr/bin/env bash
# M1.2 smoke fragment: allocate a worktree for a WorkContext, verify it, survive a
# kernel restart, release with policy=preserve, confirm the worktree is kept.
# Invoked by scripts/smoke.sh with: SOCK, DATA, TOKEN, KPID exported, and a helper
# restart_kernel function available. Exits non-zero on failure.
set -euo pipefail
V=".bin/vibe -socket $SOCK -identity local-cli -token $TOKEN"

SRC="$DATA/srcrepo"
mkdir -p "$SRC"
git -C "$SRC" -c init.defaultBranch=main init -q
echo scratch > "$SRC/README.md"
git -C "$SRC" add -A
git -C "$SRC" -c user.email=smoke@test -c user.name=smoke -c commit.gpgsign=false commit -q -m init
BASE="$(git -C "$SRC" rev-parse HEAD)"

# A WorkContext to attach it to (reuse the task the M1.1 fragment created, or make one).
create_out="$($V task create -title "ws smoke" -goal g -repo "$SRC")"
WC_ID="$(echo "$create_out" | sed -n 's/.*wc \([^ ]*\).*/\1/p')"
[ -n "$WC_ID" ] || { echo "FAIL: no wc id from: $create_out"; exit 1; }

alloc_out="$($V workspace allocate "$WC_ID" -repo "$SRC")"
WS_ID="$(echo "$alloc_out" | sed -n 's/^workspace \([^ ]*\).*/\1/p')"
WT_PATH="$(echo "$alloc_out" | sed -n 's/.*path \([^ ]*\).*/\1/p')"
WT_BRANCH="$(echo "$alloc_out" | sed -n 's/.*branch \([^ ]*\).*/\1/p')"
[ -d "$WT_PATH" ] || { echo "FAIL: worktree dir missing: $alloc_out"; exit 1; }
[ "$(git -C "$WT_PATH" rev-parse --abbrev-ref HEAD)" = "$WT_BRANCH" ] || { echo "FAIL: worktree not on $WT_BRANCH"; exit 1; }
[ "$(git -C "$WT_PATH" rev-parse HEAD)" = "$BASE" ] || { echo "FAIL: worktree not at base commit"; exit 1; }

restart_kernel

show_out="$($V workspace show "$WS_ID")"
echo "$show_out" | grep -q "$WS_ID" || { echo "FAIL: workspace lost on restart: $show_out"; exit 1; }
echo "$show_out" | grep -q 'status.*ALLOCATED' || { echo "FAIL: workspace status changed on restart: $show_out"; exit 1; }

$V workspace release "$WS_ID" -policy preserve | grep -q 'RELEASED' || { echo "FAIL: release"; exit 1; }
[ -d "$WT_PATH" ] || { echo "FAIL: preserve policy removed the worktree"; exit 1; }

echo "M1.2 WORKSPACE SMOKE: OK"
```

- [ ] **Step 4: wire it into `scripts/smoke.sh`**

`scripts/smoke.sh` currently starts the kernel, does the event.journal + M1.1 work checks, then prints `M1 SMOKE: PASSED`. Refactor the kernel start/stop into a shell function `restart_kernel` (kill `$KPID`; remove the stale socket file; relaunch with the same args; re-wait for the socket) and export `SOCK DATA TOKEN KPID` plus `restart_kernel`, then, before the final `echo "M1 SMOKE: PASSED"`, add:

```bash
source scripts/smoke-workspace.sh
```

If refactoring `restart_kernel` out of `smoke.sh` is awkward, inline the restart in `smoke-workspace.sh` instead (kill `$KPID`, `rm -f "$SOCK"`, relaunch `.bin/vibe-kernel` with the same flags used earlier in `smoke.sh`, re-wait). Either way the M1.1 checks must still pass.

- [ ] **Step 5: run the smoke** — `bash scripts/smoke.sh` → `M1.2 WORKSPACE SMOKE: OK` then `M1 SMOKE: PASSED`. Debug per FAIL lines; common issues: `local-cli` missing a `workspace.*@1` grant, a missing binding, `git` not on PATH, worktree path collision.

- [ ] **Step 6: commit**

```
feat(m1.2): M1 policy/bindings for workspace, vibe workspace subcommand, smoke
```

---

## Task 7: acceptance gate + PR

**Files:** modify `docs/M1-DESIGN.md` (§13 milestone status).

- [ ] **Step 1: full build** — `cd <repo-root> && go build github.com/example/agent-native-microkernel/... github.com/example/agent-native-os/plugins/... github.com/example/agent-native-os/cli/...` → exit 0.

- [ ] **Step 2: all Go tests** — `go test ./plugins/... ./plugins/_template ./cli/... && (cd kernel && go test ./...)` → all `ok`.

- [ ] **Step 3: kernel regression untouched** — `cd kernel && ./scripts/build.sh >/dev/null && python3 tests/integration/m05_qualification.py 2>&1 | tail -2` → `M0.5 ADVERSARIAL QUALIFICATION: PASSED`.

- [ ] **Step 4: architecture checks** — `cd <repo-root> && bash scripts/check-arch.sh` → `CONTRACT CHECK: PASSED (12 contracts, ...)`, `COMPOSITION FITNESS: PASSED (4 manifests)`, `ARCHITECTURE FITNESS: PASSED`, `ARCH CHECKS OK`.

- [ ] **Step 5: smoke** — `bash scripts/smoke.sh` → `M1.2 WORKSPACE SMOKE: OK` and `M1 SMOKE: PASSED`.

- [ ] **Step 6: G1 kernel purity**

```bash
git diff --stat c72965e HEAD -- kernel/
git diff --name-only c72965e HEAD -- kernel/internal kernel/cmd kernel/sdk
```

Expected: **both empty**. If either shows output, stop and report.

- [ ] **Step 7: update milestone status** — in `docs/M1-DESIGN.md` §13, mark `M1.2` done (append `— done (PR #N)` once the PR number is known, or `— done <short sha>`). Commit:

```
docs: M1.2 workspace-manager complete
```

- [ ] **Step 8: open the PR** — `chatgpt/m1-2-workspace-manager` → `main`, title **M1.2 — Workspace Manager (git worktree)**, body: the 7 tasks, the verbatim acceptance output from Steps 3–6, and any deviations.

---

## Self-Review

**Spec coverage (`docs/M1-DESIGN.md` §13 M1.2 = "workspace-manager（git worktree）"):**
- `workspace.allocate` → git worktree + branch + base_commit recorded → Tasks 2 (git), 3 (store), 4 (handler).
- `workspace.release(policy=preserve)` keeps the worktree (§3, §10) → Task 5.
- `workspace.get` by id / by work_context_id → Task 5.
- `WorkspaceRef` shape (§6) → Task 1 + store types.
- `permissions` declared (host-resource plane, §5.11 of the constitution / `kernel/docs/04`) → Task 5 manifest.
- G1 machine + manual → Task 7.
- Deferred correctly: the workflow plugin driving allocate→release around the pipeline (M1.6); `workspace.release` gated on seal success (M1.6 — the workflow enforces ordering, the plugin just executes); deleting worktrees in the qualification flow (M1.9 uses `policy=preserve`).

**Design note applied:** `WorkContext.active_workspace_ref` stays `null` (M1.1 shipped it as an always-null vestige). The WC ↔ workspace relationship is `WorkspaceRef.work_context_id`, answered by `workspace.get {work_context_id}` — consistent with the M1.1 decision to keep no mirror IDs on WorkContext. (`docs/M1-DESIGN.md` §6 updated in the plan commit to say so.)

**Placeholder scan:** schemas keep `workspace` as a bare `{"type":"object"}` — deliberate, shape is in the Go types + tests. No `TBD` / "handle errors" / "similar to Task N".

**Type consistency:** `WorkspaceRef` (fields `ID/WorkContextID/Repo/Path/Branch/BaseCommit/Status/ReleasePolicy/AllocatedAt/ReleasedAt`), `Status` (`StatusAllocated`/`StatusReleased`), `Store` + `Load`/`RecordAllocated`/`RecordReleased`/`GetByID`/`GetActiveByContext`, `ErrNotFound` — defined in Task 3, used in Tasks 4–5. Handlers `allocateHandler`/`releaseHandler`/`getHandler` all `func(*Store) pluginhost.Handler`. Git helpers `ensureRepo`/`resolveCommit`/`addWorktree`/`removeWorktree`/`deleteBranch` — Task 2, used in Tasks 4–5. `scratchRepo`/`gitOut` test helpers shared across `gitworktree_test.go` and `handlers_test.go` (same package `main`). Branch naming `aeos/ws-<last8>` consistent between handler and tests.
