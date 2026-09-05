# M1.8.5 — workspace.get{work_context_id} finds released workspaces — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `workspace.get{work_context_id}` deterministically return the workspace to use for a WorkContext even after it has been released with `policy=preserve`, so a cold-start caller holding only a `task_id`/`work_context_id` can rediscover a finished task's workspace. This unblocks M1.9's unconditional ADR-002 read-projection conclusion.

**Architecture:** One new pure selection function (`refLess`) plus one new `Store` method (`GetByContext`) in `plugins/workspace/store.go`; `plugins/workspace/handlers.go`'s `getHandler` switches its `work_context_id` branch to call it. A `description` string is added to the `workspace.get@1` contract schema (doc-only). `scripts/smoke-workspace.sh` gains a release→restart→by-context-lookup check. No kernel change, no contract shape/version change, no new capability.

**Tech Stack:** Go 1.19 (matches `plugins/go.mod`); bash for the acceptance script; `python3` for the contract checker (already in the repo).

**Spec:** `docs/superpowers/specs/2026-09-04-m1-8-5-workspace-by-context-recovery-design.md`

## Global Constraints

- **No kernel change.** Nothing under `kernel/` is touched.
- **No contract shape/version change.** `contracts/workspace.get/v1/schema.json` gains only a top-level `description` string; `contract`, `version` (`1.0.0`), `kind`, `request`, `response` are byte-identical to today.
- **`GetActiveByContext` is untouched** — same body, same existing test (`TestReleaseUpdatesStatusAndSurvivesReload`'s assertion that a released workspace is not "active" for its context) stays green unmodified.
- **Selection order (fixed, do not reinterpret):** if any workspace for the context has `Status == ALLOCATED`, candidates = those; else candidates = `Status == RELEASED && ReleasePolicy == "preserve"` (a `RELEASED && ReleasePolicy == "delete"` workspace is never a candidate). Among candidates: `AllocatedAt` descending, then `ReleasedAt` descending, then `ID` ascending (lexicographic, pure stable tiebreak — no time meaning). No candidates → not-ok / `NOT_FOUND`.
- **No new error class.** Never return `CONFLICT` for multiple candidates.
- **No map-iteration dependence.** Collect candidates into a slice and compare explicitly; never let Go's randomized map range order influence which ref wins a tie.
- **Two restart-verification layers, not conflated:** a Go unit test using `Load()` to replay the on-disk log (persistence, in-process) is separate from the acceptance test using the real `scripts/lib/kernel-harness.sh` `restart_kernel` (kills and restarts the actual kernel + plugin processes over the socket).
- **File scope (must equal exactly, verified in Task 5):** `plugins/workspace/store.go`, `plugins/workspace/store_test.go`, `plugins/workspace/handlers.go`, `plugins/workspace/handlers_test.go`, `contracts/workspace.get/v1/schema.json`, `scripts/smoke-workspace.sh`.
- **Commit format:** Chinese `[代码模块][类型][摘要]`, type ∈ `add|fix|refactor|chore`, continuing the convention in force since M1.8. Author `ada <oashasu@gmail.com>`. Trailer:
  ```
  Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
  Plan: docs/superpowers/plans/2026-09-04-m1-8-5-workspace-by-context-recovery.md
  ```

---

## Before Task 1: capture `BASE`

Every later reference to `"$BASE"` in this plan (Task 5) means the commit that carries this plan, its spec, and the dispatch prompt — the last commit before any Task 1–4 file is touched. Capture it once, right before starting Task 1, and persist it to a **file**, not a bare shell variable: separate steps in this plan may run in separate shells, and an exported variable does not survive across them.

```bash
git rev-parse HEAD > /tmp/m185-base.txt
cat /tmp/m185-base.txt   # confirm: exactly one 40-char SHA, nothing else
```

Every later step that needs `$BASE` re-reads it fresh:
```bash
BASE="$(cat /tmp/m185-base.txt)"
```
never a bare `$BASE` assumed to already be set in the current shell.

---

### Task 1: `GetByContext` selection logic

**Files:**
- Modify: `plugins/workspace/store.go`
- Test: `plugins/workspace/store_test.go`

**Interfaces:**
- Consumes: existing `Store` (`byID map[string]*WorkspaceRef`, `mu sync.Mutex`), `WorkspaceRef{ID, WorkContextID, Status, ReleasePolicy, AllocatedAt, ReleasedAt, ...}`, `Status` constants `StatusAllocated`/`StatusReleased`, existing `RecordAllocated(ref WorkspaceRef) error` (records the ref exactly as given — including a pre-set `Status`/`ReleasedAt`, which lets tests seed released-looking refs directly without a two-step allocate+release), `cloneRef(*WorkspaceRef) WorkspaceRef`.
- Produces: `func (s *Store) GetByContext(wcID string) (WorkspaceRef, bool)` and `func refLess(a, b *WorkspaceRef) bool` (package-private, same package as the tests) — Task 2 calls `GetByContext`.

- [ ] **Step 1: Write the failing tests**

Append to `plugins/workspace/store_test.go`:

```go
func TestRefLessOrdering(t *testing.T) {
	cases := []struct {
		name        string
		a, b        WorkspaceRef
		wantBBetter bool
	}{
		{"later AllocatedAt wins", WorkspaceRef{ID: "a", AllocatedAt: "t0"}, WorkspaceRef{ID: "b", AllocatedAt: "t1"}, true},
		{"earlier AllocatedAt loses", WorkspaceRef{ID: "a", AllocatedAt: "t1"}, WorkspaceRef{ID: "b", AllocatedAt: "t0"}, false},
		{"equal AllocatedAt, later ReleasedAt wins", WorkspaceRef{ID: "a", AllocatedAt: "t0", ReleasedAt: "r0"}, WorkspaceRef{ID: "b", AllocatedAt: "t0", ReleasedAt: "r1"}, true},
		{"equal AllocatedAt and ReleasedAt, smaller ID wins", WorkspaceRef{ID: "ws-b", AllocatedAt: "t0", ReleasedAt: "r0"}, WorkspaceRef{ID: "ws-a", AllocatedAt: "t0", ReleasedAt: "r0"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, b := c.a, c.b
			if got := refLess(&a, &b); got != c.wantBBetter {
				t.Fatalf("refLess(a=%+v, b=%+v) = %v, want %v", a, b, got, c.wantBBetter)
			}
		})
	}
}

func TestGetByContextPrefersLatestAllocated(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_ = s.RecordAllocated(WorkspaceRef{ID: "ws-1", WorkContextID: "wc-1", Status: StatusAllocated, AllocatedAt: "t0"})
	_ = s.RecordAllocated(WorkspaceRef{ID: "ws-2", WorkContextID: "wc-1", Status: StatusAllocated, AllocatedAt: "t1"})
	got, ok := s.GetByContext("wc-1")
	if !ok || got.ID != "ws-2" {
		t.Fatalf("want ws-2 (latest allocated), got %+v ok=%v", got, ok)
	}
}

func TestGetByContextPrefersAllocatedOverNewerPreserveReleased(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_ = s.RecordAllocated(WorkspaceRef{ID: "ws-1", WorkContextID: "wc-1", Status: StatusAllocated, AllocatedAt: "t0"})
	_ = s.RecordAllocated(WorkspaceRef{ID: "ws-2", WorkContextID: "wc-1", Status: StatusReleased, ReleasePolicy: "preserve", AllocatedAt: "t1"})
	got, ok := s.GetByContext("wc-1")
	if !ok || got.ID != "ws-1" {
		t.Fatalf("want ws-1 (ALLOCATED beats a RELEASED+preserve with a *later* AllocatedAt — priority is by status first, not by timestamp), got %+v ok=%v", got, ok)
	}
}

func TestGetByContextFallsBackToLatestPreserveReleased(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_ = s.RecordAllocated(WorkspaceRef{ID: "ws-1", WorkContextID: "wc-1", Status: StatusAllocated, AllocatedAt: "t0"})
	_ = s.RecordAllocated(WorkspaceRef{ID: "ws-2", WorkContextID: "wc-1", Status: StatusAllocated, AllocatedAt: "t1"})
	if _, err := s.RecordReleased("ws-1", "preserve"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordReleased("ws-2", "preserve"); err != nil {
		t.Fatal(err)
	}
	got, ok := s.GetByContext("wc-1")
	if !ok || got.ID != "ws-2" {
		t.Fatalf("want ws-2 (latest allocated among the preserved), got %+v ok=%v", got, ok)
	}
}

func TestGetByContextSkipsDeletedReleased(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_ = s.RecordAllocated(WorkspaceRef{ID: "ws-1", WorkContextID: "wc-1", Status: StatusAllocated, AllocatedAt: "t0"})
	_ = s.RecordAllocated(WorkspaceRef{ID: "ws-2", WorkContextID: "wc-1", Status: StatusAllocated, AllocatedAt: "t1"})
	if _, err := s.RecordReleased("ws-1", "preserve"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordReleased("ws-2", "delete"); err != nil {
		t.Fatal(err)
	}
	got, ok := s.GetByContext("wc-1")
	if !ok || got.ID != "ws-1" {
		t.Fatalf("want ws-1 (only preserve candidate; ws-2 is delete-policy), got %+v ok=%v", got, ok)
	}
}

func TestGetByContextTiebreaksOnReleasedAtWhenAllocatedAtEqual(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_ = s.RecordAllocated(WorkspaceRef{ID: "ws-1", WorkContextID: "wc-1", Status: StatusReleased, ReleasePolicy: "preserve", AllocatedAt: "t0", ReleasedAt: "r0"})
	_ = s.RecordAllocated(WorkspaceRef{ID: "ws-2", WorkContextID: "wc-1", Status: StatusReleased, ReleasePolicy: "preserve", AllocatedAt: "t0", ReleasedAt: "r1"})
	got, ok := s.GetByContext("wc-1")
	if !ok || got.ID != "ws-2" {
		t.Fatalf("want ws-2 (later ReleasedAt breaks the AllocatedAt tie), got %+v ok=%v", got, ok)
	}
}

func TestGetByContextTiebreaksOnIDWhenAllTimestampsEqual(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_ = s.RecordAllocated(WorkspaceRef{ID: "ws-b", WorkContextID: "wc-1", Status: StatusReleased, ReleasePolicy: "preserve", AllocatedAt: "t0", ReleasedAt: "r0"})
	_ = s.RecordAllocated(WorkspaceRef{ID: "ws-a", WorkContextID: "wc-1", Status: StatusReleased, ReleasePolicy: "preserve", AllocatedAt: "t0", ReleasedAt: "r0"})
	got, ok := s.GetByContext("wc-1")
	if !ok || got.ID != "ws-a" {
		t.Fatalf("want ws-a (smaller ID wins a full tie), got %+v ok=%v", got, ok)
	}
}

func TestGetByContextNoCandidatesNotFound(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_ = s.RecordAllocated(WorkspaceRef{ID: "ws-1", WorkContextID: "wc-1", Status: StatusReleased, ReleasePolicy: "delete", AllocatedAt: "t0"})
	if _, ok := s.GetByContext("wc-1"); ok {
		t.Fatalf("delete-policy-only context must yield no candidate")
	}
	if _, ok := s.GetByContext("wc-does-not-exist"); ok {
		t.Fatalf("unknown context must yield no candidate")
	}
}

func TestGetByContextSurvivesReload(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	_ = s.RecordAllocated(WorkspaceRef{ID: "ws-1", WorkContextID: "wc-1", Status: StatusAllocated, AllocatedAt: "t0"})
	if _, err := s.RecordReleased("ws-1", "preserve"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.GetByContext("wc-1")
	if !ok || got.ID != "ws-1" || got.Status != StatusReleased {
		t.Fatalf("GetByContext after reload: %+v ok=%v", got, ok)
	}
}
```

- [ ] **Step 2: Run — verify the new tests fail to compile**

Run: `go test ./plugins/workspace/... -run 'TestRefLess|TestGetByContext' -v`
Expected: build failure — `undefined: refLess` and `undefined: (*Store).GetByContext` (or similar — `GetByContext`/`refLess` do not exist yet). Confirm it fails for *this* reason, not a typo in the test code.

- [ ] **Step 3: Implement `GetByContext` and `refLess`**

Add to `plugins/workspace/store.go`, after the existing `GetActiveByContext` method (do not modify `GetActiveByContext` itself):

```go
// GetByContext returns the workspace a cold-start caller holding only wcID
// should use: the most recently allocated ALLOCATED workspace if one exists,
// else the most recently allocated RELEASED+preserve workspace. A
// RELEASED+delete workspace is never a candidate — its worktree is gone.
// The tiebreak (AllocatedAt desc, then ReleasedAt desc, then ID asc) is
// evaluated over an explicit slice, never over map iteration order, so the
// result is deterministic even when timestamps collide.
func (s *Store) GetByContext(wcID string) (WorkspaceRef, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var allocated, preserved []*WorkspaceRef
	for _, ref := range s.byID {
		if ref.WorkContextID != wcID {
			continue
		}
		switch {
		case ref.Status == StatusAllocated:
			allocated = append(allocated, ref)
		case ref.Status == StatusReleased && ref.ReleasePolicy == "preserve":
			preserved = append(preserved, ref)
		}
	}
	candidates := allocated
	if len(candidates) == 0 {
		candidates = preserved
	}
	if len(candidates) == 0 {
		return WorkspaceRef{}, false
	}
	best := candidates[0]
	for _, ref := range candidates[1:] {
		if refLess(best, ref) {
			best = ref
		}
	}
	return cloneRef(best), true
}

// refLess reports whether b should replace a as the current best under the
// AllocatedAt-desc / ReleasedAt-desc / ID-asc order. ID is a pure stable
// tiebreak with no time meaning.
func refLess(a, b *WorkspaceRef) bool {
	if a.AllocatedAt != b.AllocatedAt {
		return b.AllocatedAt > a.AllocatedAt
	}
	if a.ReleasedAt != b.ReleasedAt {
		return b.ReleasedAt > a.ReleasedAt
	}
	return b.ID < a.ID
}
```

- [ ] **Step 4: Run — verify green, and that nothing else broke**

Run: `go test ./plugins/workspace/... -v`
Expected: every test passes, including the pre-existing `TestReleaseUpdatesStatusAndSurvivesReload` (unchanged — still asserts `GetActiveByContext` returns not-ok for a released workspace) and `TestGetBySelector`.

- [ ] **Step 5: 致残对照 — drop the delete-policy filter (M1)**

Temporarily change the `preserved` case to also match `delete`:
```go
case ref.Status == StatusReleased:
	preserved = append(preserved, ref)
```
(delete the `&& ref.ReleasePolicy == "preserve"` condition). Run:
`go test ./plugins/workspace/... -run TestGetByContextSkipsDeletedReleased -v`
Expected: FAIL (`want ws-1 ..., got {ID:ws-2 ...}` — a delete-policy ref is now wrongly returned). Restore the original `case ref.Status == StatusReleased && ref.ReleasePolicy == "preserve":` line exactly, then re-run the same command and confirm PASS.

- [ ] **Step 6: 致残对照 — prefer RELEASED over ALLOCATED (M2)**

Temporarily swap the candidate selection to:
```go
candidates := preserved
if len(candidates) == 0 {
	candidates = allocated
}
```
Run: `go test ./plugins/workspace/... -run TestGetByContextPrefersAllocatedOverNewerPreserveReleased -v`
Expected: FAIL — the mutated code picks the RELEASED+preserve ref (`ws-2`, whose `AllocatedAt` is later) instead of the ALLOCATED one (`ws-1`).
(`TestGetByContextPrefersLatestAllocated` is **not** a valid witness for this mutation: it has no RELEASED workspace at all, so `preserved` is empty and the mutated code falls back to `allocated` — same result, silently passing either way. Do not use it here.)
Restore the original `candidates := allocated` / fallback-to-`preserved` code exactly, re-run, confirm PASS.

- [ ] **Step 7: 致残对照 — drop the ID tiebreak (M3)**

Temporarily change `refLess`'s final line from `return b.ID < a.ID` to `return false`. Run:
`go test ./plugins/workspace/... -run TestRefLessOrdering -v`
Expected: FAIL — specifically the `"equal AllocatedAt and ReleasedAt, smaller ID wins"` subtest (`refLess` now always returns `false` on a full tie, not `true`). This direct test of the pure comparator is the **only** required failure witness for M3, because it has no dependency on map iteration order.
(`TestGetByContextTiebreaksOnIDWhenAllTimestampsEqual`'s outcome under this mutation is a **coincidence, not proof**: with the tiebreak gone, `best` never advances past `candidates[0]`, whose identity depends on `s.byID`'s map range order — Go's map iteration is randomized per run, so that test might still print `ws-a` and pass by chance. Do not claim it as evidence for M3, in either direction.)
Restore `return b.ID < a.ID`, re-run `TestRefLessOrdering`, confirm PASS.

- [ ] **Step 8: Commit**

```bash
git add plugins/workspace/store.go plugins/workspace/store_test.go
git commit -m "[工作空间][add][新增按上下文查找已释放workspace的选择逻辑]

新增 GetByContext：ALLOCATED 优先，否则取最新的 RELEASED+preserve；
delete 策略的已释放 workspace 永不作为候选。tiebreak 固定为
AllocatedAt 降序、ReleasedAt 降序、ID 升序（纯稳定兜底，不带时间语义），
比较基于显式 slice，不依赖 map 遍历顺序。GetActiveByContext 原样不动。

Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
Plan: docs/superpowers/plans/2026-09-04-m1-8-5-workspace-by-context-recovery.md"
```

---

### Task 2: Wire `getHandler` to `GetByContext`

**Files:**
- Modify: `plugins/workspace/handlers.go`
- Test: `plugins/workspace/handlers_test.go`

**Interfaces:**
- Consumes: `Store.GetByContext` (Task 1), existing `getHandler(s *Store) pluginhost.Handler`, `getRequest{WorkspaceID, WorkContextID}`, existing test helpers `scratchRepo(t)`, `fencedEnv(t, dir)`, `allocate(t, s, dir, repo, wc) WorkspaceRef`, `releaseHandler(s *Store) pluginhost.Handler`.
- Produces: `getHandler`'s `work_context_id` branch now returns a released-but-preserved workspace instead of `NOT_FOUND`. No signature changes.

- [ ] **Step 1: Write the failing tests**

Append to `plugins/workspace/handlers_test.go`:

```go
func TestGetHandlerByContextReturnsReleasedWorkspace(t *testing.T) {
	repo, _ := scratchRepo(t)
	dir := t.TempDir()
	s, _ := Load(dir)
	ws := allocate(t, s, dir, repo, "wc-1")

	env := fencedEnv(t, dir)
	env.Payload = protocol.NewPayload(map[string]string{"workspace_id": ws.ID, "policy": "preserve"})
	if _, perr := releaseHandler(s)(env); perr != nil {
		t.Fatalf("release: %+v", perr)
	}

	out, perr := getHandler(s)(protocol.Envelope{Payload: protocol.NewPayload(map[string]string{"work_context_id": "wc-1"})})
	if perr != nil {
		t.Fatalf("get by context after release: %+v", perr)
	}
	var r struct {
		Workspace WorkspaceRef `json:"workspace"`
	}
	b, _ := json.Marshal(out)
	_ = json.Unmarshal(b, &r)
	if r.Workspace.ID != ws.ID || r.Workspace.Status != StatusReleased {
		t.Fatalf("want the released workspace, got %+v", r.Workspace)
	}
}

func TestGetHandlerByContextNotFoundAfterDeleteRelease(t *testing.T) {
	repo, _ := scratchRepo(t)
	dir := t.TempDir()
	s, _ := Load(dir)
	ws := allocate(t, s, dir, repo, "wc-1")

	env := fencedEnv(t, dir)
	env.Payload = protocol.NewPayload(map[string]string{"workspace_id": ws.ID, "policy": "delete"})
	if _, perr := releaseHandler(s)(env); perr != nil {
		t.Fatalf("release: %+v", perr)
	}

	_, perr := getHandler(s)(protocol.Envelope{Payload: protocol.NewPayload(map[string]string{"work_context_id": "wc-1"})})
	if perr == nil || perr.Code != "NOT_FOUND" {
		t.Fatalf("want NOT_FOUND after delete-policy release with no other workspace, got %+v", perr)
	}
}
```

- [ ] **Step 2: Run — verify it fails for the right reason**

Run: `go test ./plugins/workspace/... -run TestGetHandlerByContext -v`
Expected: `TestGetHandlerByContextReturnsReleasedWorkspace` FAILs with `perr: &{Code:NOT_FOUND ...}` (the handler still calls `GetActiveByContext`, which excludes released workspaces). `TestGetHandlerByContextNotFoundAfterDeleteRelease` passes already (both old and new code return NOT_FOUND here) — that is expected; it exists to be protected by Task 1's Step 7-equivalent mutation below, not to prove new behavior on its own.

- [ ] **Step 3: Implement**

In `plugins/workspace/handlers.go`, in `getHandler`, change:
```go
			ref, ok = s.GetActiveByContext(q.WorkContextID)
```
to:
```go
			ref, ok = s.GetByContext(q.WorkContextID)
```

- [ ] **Step 4: Run — verify green**

Run: `go test ./plugins/workspace/... -v`
Expected: all tests pass, including `TestGetBySelector` (unaffected — it queries immediately after allocation, before any release) and both new tests above.

- [ ] **Step 5: 致残对照 — revert the handler wiring (M4)**

Temporarily change the line back to `ref, ok = s.GetActiveByContext(q.WorkContextID)`. Run:
`go test ./plugins/workspace/... -run TestGetHandlerByContextReturnsReleasedWorkspace -v`
Expected: FAIL. Restore `s.GetByContext(q.WorkContextID)`, re-run, confirm PASS.

- [ ] **Step 6: Commit**

```bash
git add plugins/workspace/handlers.go plugins/workspace/handlers_test.go
git commit -m "[工作空间][fix][按work_context_id查询时接入GetByContext]

getHandler 的 work_context_id 分支从 GetActiveByContext 切到 GetByContext，
使已用 preserve 策略释放的 workspace 也能按上下文查到；错误形状不变，
仍是 NOT_FOUND。workspace_id 分支（GetByID）不受影响。

Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
Plan: docs/superpowers/plans/2026-09-04-m1-8-5-workspace-by-context-recovery.md"
```

---

### Task 3: Contract documentation (no shape change)

**Files:**
- Modify: `contracts/workspace.get/v1/schema.json`

**Interfaces:**
- Consumes: none (doc-only).
- Produces: nothing new consumed by later tasks — this task is independent and verifies itself against `scripts/check-contracts.py`.

- [ ] **Step 1: Add the `description` field**

Current file:
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "contract": "workspace.get@1",
  "version": "1.0.0",
  "kind": "query",
  "compatibility": "backward-within-major",
  "request": {"type":"object","additionalProperties":false,"properties":{"workspace_id":{"type":"string"},"work_context_id":{"type":"string"}},"required":[]},
  "response": {"type":"object","additionalProperties":true,"properties":{"workspace":{"type":"object"}},"required":["workspace"]}
}
```
Replace it with (only the `description` line is new; every other field is byte-identical):
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "contract": "workspace.get@1",
  "version": "1.0.0",
  "kind": "query",
  "description": "A work_context_id lookup returns the workspace to use for that context: the currently ALLOCATED one if any, else the most recently allocated RELEASED workspace whose release policy was 'preserve'. Callers must check response.workspace.status — the result may be RELEASED, not just ALLOCATED. A workspace_id lookup is unaffected.",
  "compatibility": "backward-within-major",
  "request": {"type":"object","additionalProperties":false,"properties":{"workspace_id":{"type":"string"},"work_context_id":{"type":"string"}},"required":[]},
  "response": {"type":"object","additionalProperties":true,"properties":{"workspace":{"type":"object"}},"required":["workspace"]}
}
```

- [ ] **Step 2: Prove the edit added *only* `description`**

`check-contracts.py` (Step 3) only validates that `request`/`response` are legal JSON Schema and that the catalog count is right — it does not catch an accidental change to `additionalProperties`, `required`, or a field inside `request`/`response`. Prove the shape is otherwise byte-identical by diffing the parsed JSON against the pre-edit commit (run this **before** committing, while `HEAD` is still the commit prior to this edit):

```bash
python3 -c "
import json, subprocess
old = json.loads(subprocess.check_output(['git', 'show', 'HEAD:contracts/workspace.get/v1/schema.json']))
new = json.load(open('contracts/workspace.get/v1/schema.json'))
new_without_description = {k: v for k, v in new.items() if k != 'description'}
assert old == new_without_description, 'contract changed beyond adding description: old=%r new(minus description)=%r' % (old, new_without_description)
assert isinstance(new.get('description'), str) and new['description'], 'description missing or empty'
print('SCHEMA_SHAPE_UNCHANGED_OK')
"
```
Expected: `SCHEMA_SHAPE_UNCHANGED_OK`. If the assertion fails, the edit touched something beyond `description` — revert and redo Step 1 exactly as written above.

- [ ] **Step 3: Verify the contract checker still passes with the same count**

Run: `python3 scripts/check-contracts.py --root contracts`
Expected: `CONTRACT CHECK: PASSED (31 contracts, root=<absolute path to contracts>)` — same count as before this change (31). If the count differs, the edit broke JSON syntax or catalog identity; fix and re-run.

- [ ] **Step 4: Commit**

```bash
git add contracts/workspace.get/v1/schema.json
git commit -m "[契约][chore][workspace.get补充按上下文可能返回RELEASED的说明]

只加一个顶层 description 字符串，contract/version/kind/request/response
逐字节不变。check-contracts.py 只校验 contract/version/kind/request与
response 的JSON Schema合法性，不检查未知顶层键，契约数仍是31，
不是schema语义改动。

Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
Plan: docs/superpowers/plans/2026-09-04-m1-8-5-workspace-by-context-recovery.md"
```

---

### Task 4: Acceptance test — release → restart → find-by-context

**Files:**
- Modify: `scripts/smoke-workspace.sh`

**Interfaces:**
- Consumes: `$SOCK`/`$DATA`/`$TOKEN`/`$DEV_TOKEN`/`restart_kernel` (from `scripts/lib/kernel-harness.sh`, already sourced by the parent `scripts/smoke.sh` before this fragment is sourced), the file's own existing `$V`, `$WC_ID`, `$WS_ID`, `$WT_PATH` variables (already set earlier in the same file), `vibe workspace show -work-context <wc>` (existing CLI flag, `cli/vibe/main.go:319` — prints `id`/`status`/`branch`/`path`/`base_commit` on separate lines). `local-cli`'s grants in `config/m1-policy.json` already include `workspace.get@1` — no policy/bindings change needed.
- Produces: a new smoke marker line `M1.8.5 WORKSPACE-BY-CONTEXT-RECOVERY SMOKE: OK`, printed after the pre-existing `M1.2 WORKSPACE SMOKE: OK` line (kept, unmodified, at its original position — nothing else in the repo depends on it being the file's last line).

**This file cannot be run standalone** — it is `source`d by `scripts/smoke.sh`, which sets up `$SOCK`/`$DATA`/tokens and calls `build_bins`/`restart_kernel` first. Always test via `bash scripts/smoke.sh`.

**Identity for the new check:** the existing `$V` in this file is `m1-dev`, a broadly-provisioned identity used for the *write* side of this fragment (allocate/release). The read that matters for M1.9's Console story is whether a caller with only query-level grants can rediscover the workspace — so the new check uses a **separate, read-only-scoped `local-cli` command variable**, not `$V`. `local-cli`'s existing grants already include `workspace.get@1` (`config/m1-policy.json`), so this needs no policy/bindings change.

- [ ] **Step 1: Write the failing check**

Append to the end of `scripts/smoke-workspace.sh` (after the existing `echo "M1.2 WORKSPACE SMOKE: OK"` line):

```bash

VQ=".bin/vibe -socket $SOCK -identity local-cli -token $TOKEN"   # read-only query identity — see Task 4 Interfaces
restart_kernel

byctx_out=""
for _ in $(seq 1 50); do
  byctx_out="$($VQ workspace show -work-context "$WC_ID" 2>/dev/null || true)"
  case "$byctx_out" in *"id $WS_ID"*"status RELEASED"*) break ;; esac
  sleep 0.1
done
case "$byctx_out" in
  *"id $WS_ID"*"status RELEASED"*) : ;;
  *) echo "FAIL: work_context_id lookup after release+restart did not return the released workspace: $byctx_out"; exit 1 ;;
esac
[ -d "$WT_PATH" ] || { echo "FAIL: preserve policy worktree missing after second restart"; exit 1; }

echo "M1.8.5 WORKSPACE-BY-CONTEXT-RECOVERY SMOKE: OK"
```

Also update the file's header comment (currently lines 1-3, "M1.2 smoke fragment: ...") to add one sentence noting the M1.8.5 addition, e.g. append: `Also (M1.8.5): after that release, restart the kernel again and confirm the read-only local-cli identity's workspace.get{work_context_id} still finds it (status RELEASED).`

Since Task 1/2's implementation is not yet what this step is testing in isolation — the fragment can only be exercised through the full `scripts/smoke.sh` — this step's "run to see it fail" is the same command as Step 2 below, run *before* Tasks 1–3 are committed. Since Tasks 1–3 are already committed by the time you reach Task 4 in this plan's order, do the red/green proof by temporarily reverting Task 2's one-line handler change (not by reverting Task 1–3 wholesale):

- [ ] **Step 2: Run — verify it fails for the right reason**

Temporarily edit `plugins/workspace/handlers.go` back to `ref, ok = s.GetActiveByContext(q.WorkContextID)`, rebuild (`bash scripts/build.sh >/dev/null`), then run:
`bash scripts/smoke.sh`
Expected: FAIL at the new check — `FAIL: work_context_id lookup after release+restart did not return the released workspace: ...` (the response has no `id $WS_ID` line at all, since the old code returns `NOT_FOUND`).
Restore `ref, ok = s.GetByContext(q.WorkContextID)` and rebuild (`bash scripts/build.sh >/dev/null`) before continuing.

- [ ] **Step 3: Run — verify green**

Run: `bash scripts/smoke.sh`
Expected: `M1.2 WORKSPACE SMOKE: OK`, then `M1.8.5 WORKSPACE-BY-CONTEXT-RECOVERY SMOKE: OK`, and the run continues through the other fragments to `M1 SMOKE: PASSED`.

- [ ] **Step 4: Commit**

```bash
git add scripts/smoke-workspace.sh
git commit -m "[验收脚本][add][补充release后重启仍能按上下文找回workspace的验收]

在原有 M1.2 释放校验之后追加：再次 restart_kernel，按 work_context_id
查询，断言返回的就是刚释放的 workspace 且 status 为 RELEASED；worktree
文件仍在（preserve策略）。原有 M1.2 校验和其标记行位置不变。

Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
Plan: docs/superpowers/plans/2026-09-04-m1-8-5-workspace-by-context-recovery.md"
```

---

### Task 5: Full acceptance + 致残 sweep + scope check

**Files:** none changed (verification only).

- [ ] **Step 1: Full test suite + race**

```bash
go test ./plugins/... ./plugins/_template ./cli/... && ( cd kernel && go test ./... ) && echo GO_TESTS_OK
go test -race ./plugins/workspace/ && echo RACE_OK
```
Expected: `GO_TESTS_OK`, `RACE_OK`.

- [ ] **Step 2: Architecture + contracts unchanged**

```bash
bash scripts/check-arch.sh
```
Expected: `CONTRACT CHECK: PASSED (31 contracts...)`, `COMPOSITION FITNESS: PASSED (10 manifests)`, `ARCH CHECKS OK` — identical counts to before this milestone.

- [ ] **Step 3: Full smoke ×5, no orphans**

```bash
for i in 1 2 3 4 5; do
  bash scripts/smoke.sh >/tmp/m185-smoke-$i.log 2>&1
  rc=$?
  { [ "$rc" -eq 0 ] && grep -qx 'M1 SMOKE: PASSED' "/tmp/m185-smoke-$i.log" && ! grep -q FAIL "/tmp/m185-smoke-$i.log"; } \
    && echo "smoke $i OK" || { echo "smoke $i rc=$rc"; tail -30 "/tmp/m185-smoke-$i.log"; exit 1; }
done
orph="$(ps -axo pid=,comm= | awk '$2 ~ /(^|\/)(vibe-kernel|agent-harness|artifact|blob|engineering-work|event-journal|review|session|tool-runner|work-registry|workspace)([[:space:]]|$)/')"
[ -z "$orph" ] && echo NO_ORPHANS || { echo "orphans:"; printf '%s\n' "$orph"; exit 1; }
```
Expected: `smoke 1 OK` … `smoke 5 OK`, `NO_ORPHANS`.

- [ ] **Step 4: Re-confirm the four 致残 mutations from Tasks 1–2**

Re-run, in order, the exact mutate → run → expect-FAIL → restore → run → expect-PASS sequences from Task 1 Steps 5–7 and Task 2 Step 5 (M1–M4). This is the independent reviewer's re-sweep, not a re-description — actually perform each mutation again with a manual edit and its exact manual reversal (the same discipline as every prior mutation step in this plan). After the sweep, confirm:
```bash
git status --porcelain
```
Expected: empty (all four temporary mutations were reverted).

- [ ] **Step 5: Scope check**

```bash
BASE="$(cat /tmp/m185-base.txt)"   # captured in "Before Task 1: capture BASE"
git diff --name-only "$BASE" HEAD
```
Expected exactly:
```
contracts/workspace.get/v1/schema.json
plugins/workspace/handlers.go
plugins/workspace/handlers_test.go
plugins/workspace/store.go
plugins/workspace/store_test.go
scripts/smoke-workspace.sh
```
No `kernel/`, no other plugin, no `docs/M1-DESIGN.md`, no `docs/superpowers/**` (those are already part of `$BASE`, not this diff).

- [ ] **Step 6: Report**

State: final commit SHA, the six-file diff above confirmed exact, all of Steps 1–4 green, and that `scripts/smoke-workspace.sh`'s new check (`M1.8.5 WORKSPACE-BY-CONTEXT-RECOVERY SMOKE: OK`) is present in a fresh `bash scripts/smoke.sh` run's output. Do **not** open a PR from a dispatched sandbox unless the dispatch prompt says to — the reviewer merges after an independent re-run (same pattern as M1.1–M1.6, M1.8).

---

## Self-Review

**1. Spec coverage** — invariant 0 (no kernel/contract-shape change): Task 3 Step 2 (structural diff) + Task 5 Steps 2/5. Invariant 1 (`GetActiveByContext` untouched): Task 1 Step 4 re-runs its test unmodified. Invariant 2 (selection order): Task 1 Steps 1/3, with the ALLOCATED-priority claim specifically witnessed by `TestGetByContextPrefersAllocatedOverNewerPreserveReleased` (not the same-status-only `TestGetByContextPrefersLatestAllocated`). Invariant 3 (no `CONFLICT`): implementation in Task 1 Step 3 never returns an error from `GetByContext` other than the `bool`; `getHandler` unchanged for that path. Invariant 4 (`ReleasedAt` not primary): Task 1 `refLess`. Invariant 5 (two verification layers): Task 1's `TestGetByContextSurvivesReload` (layer 1) vs. Task 4's `restart_kernel` acceptance check using the `local-cli` read-only identity (layer 2). §5 (致残 M1–M4): Task 1 Steps 5–7 (M3's sole valid witness is `TestRefLessOrdering`, not the map-order-dependent `GetByContext`-level test), Task 2 Step 5, re-verified in Task 5 Step 4. §6 acceptance items 1–7: Task 5 Steps 1–6. The "Before Task 1: capture BASE" preamble makes Task 5 Step 5's whitelist check reproducible across separate shells.

**2. Placeholder scan** — every step has literal Go/bash/JSON/Python; no "TBD"/"handle edge cases"; the one deliberately-explained deviation from the bite-sized template (Task 4 Step 1's note about deferring the red-proof to Step 2) states exactly what to do, not a vague deferral.

**3. Type/name consistency** — `GetByContext(wcID string) (WorkspaceRef, bool)` matches its Task 1 definition and Task 2's handler call; `refLess(a, b *WorkspaceRef) bool` matches its one call site in `GetByContext` and its direct test. `getHandler`'s `ref, ok = s.GetByContext(q.WorkContextID)` matches the existing `var ref WorkspaceRef; var ok bool` declared above it in the handler (unchanged). Test helpers (`scratchRepo`, `fencedEnv`, `allocate`) are consumed with their existing signatures, not redefined. Task 4's new `$VQ` variable is named distinctly from the file's existing `$V` (`m1-dev`) so neither shadows the other.

**4. Fixed after independent review (2026-09-05):** (a) added `TestGetByContextPrefersAllocatedOverNewerPreserveReleased` — the original M2 witness (`TestGetByContextPrefersLatestAllocated`) had no RELEASED candidate, so inverting the ALLOCATED/preserved priority silently fell through to the same answer and would not have failed; (b) added the "Before Task 1: capture BASE" preamble — Task 5 previously referenced `"$BASE"` with no step defining it, and a bare env var would not survive across separate shell invocations anyway; (c) Task 4's new check now uses a dedicated `local-cli` (`$VQ`) identity instead of reusing `$V` (`m1-dev`) — `local-cli` already holds `workspace.get@1` in `config/m1-policy.json`, so this needed no policy change, and it validates the query path a real read-only Console caller would actually use; (d) Task 1 Step 7 no longer claims the `GetByContext`-level test fails under the M3 mutation — only `TestRefLessOrdering`'s direct subtest is a valid witness, since dropping the ID tiebreak makes the `GetByContext`-level result depend on Go's randomized map iteration order; (e) Task 3 gained a Step 2 that diffs the parsed JSON against the pre-edit commit, so "only `description` changed" is proven, not asserted.
