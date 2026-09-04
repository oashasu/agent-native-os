# M1.8.5 — workspace.get{work_context_id} finds released workspaces — Design

**Status:** for review (2026-09-04)
**Spec source:** ADR-002 (Console read-projection sufficiency — cold-start, task-only navigation of an already-released workspace), `docs/M1-DESIGN.md` §6/§13.
**Milestone position:** between M1.8 (`m1.8-real-provider-adapter`, main `c640957`) and M1.9. M1.9's ADR-002 conclusion depends on this milestone landing first.
**Execution:** dispatched to ChatGPT (single plugin, no credentials, no real external dependency, fully deterministic tests) — same pattern as M1.1–M1.6. Selection semantics are locked in this spec; the implementer does not make architecture decisions.

---

## 1. Goal

Today, `workspace.get{work_context_id}` calls `Store.GetActiveByContext`, which only returns a workspace whose `Status == ALLOCATED`. Once a workflow calls `workspace.release`, that workspace becomes unreachable by `work_context_id` — only its `workspace_id` (recorded at allocation time) still finds it. A cold-start Console, holding only a `task_id`/`work_context_id`, cannot rediscover the workspace of a *finished* task. M1.9's ADR-002 read-projection acceptance is blocked on this: it can only reach a CONDITIONAL result (proven given a pre-captured `workspace_id`), not the unconditional claim ADR-002 makes.

M1.8.5 closes that gap: `workspace.get{work_context_id}` deterministically returns the workspace to use for that context, whether it is currently allocated or was released with `policy=preserve`. Nothing else changes — no new contract, no new capability, no kernel change.

## 2. Design invariants (must hold at merge)

0. **No kernel change. No contract/schema/version change.** `contracts/workspace.get/v1/schema.json` gains at most a `description` string (§4.3) — not a new field, not a version bump, not a request/response shape change. `check-contracts.py`'s validation (`contract`/`version`/`kind`/request+response-are-valid-JSON-Schema) does not inspect unknown top-level keys, so this is safe; contract count stays 31.
1. **`GetActiveByContext` is unchanged.** Its behavior (return only `Status==ALLOCATED`, latest `AllocatedAt`, else not-ok) and its existing test (`store_test.go` — asserts a released workspace is *not* "active" for its context) both stay exactly as they are. A new method, `GetByContext`, is added alongside it; `getHandler`'s `work_context_id` branch switches to call `GetByContext` instead.
2. **Deterministic selection order (fixed, no reinterpretation):**
   1. If any workspace for this `work_context_id` has `Status == ALLOCATED`: candidates = those; else candidates = those with `Status == RELEASED && ReleasePolicy == "preserve"` (a `RELEASED && ReleasePolicy == "delete"` workspace is never a candidate — its worktree is gone).
   2. Among candidates, sort by: `AllocatedAt` **descending**, then `ReleasedAt` **descending**, then `ID` **ascending** (lexicographic). Return the first.
   3. No candidates → not-ok (handler returns `NOT_FOUND`, same error shape as today).
   `ID` is a **stable tiebreak only** — it carries no time semantics and is compared purely as a string; it exists so the result is deterministic even if `AllocatedAt`/`ReleasedAt` collide (same-instant allocations, or comparing an ALLOCATED ref whose `ReleasedAt == ""` against another with `ReleasedAt == ""`).
   **No map iteration is used to pick the winner** — collect candidates into a slice and compare explicitly; Go map range order is randomized and must never be allowed to influence which workspace is returned when multiple candidates tie.
3. **No new error class.** A context with multiple candidates never returns `CONFLICT` — the API's existing contract is "deterministically pick one," and introducing a new error path for an already-ambiguous-by-design query would be asymmetric with `GetByID`'s and the current `GetActiveByContext`'s behavior.
4. **`ReleasedAt` is not a primary sort key.** It only breaks an `AllocatedAt` tie. A future need for full history or release-time filtering is a new `.query`-style capability, not a further broadening of `.get`'s implicit semantics (§8 NON-GOALS).
5. **Two separate verification layers for "survives restart" — do not conflate them:**
   - **Persistence (Go unit test):** `Load()` replays the on-disk JSONL log in-process and the reloaded `Store`'s `GetByContext` returns the expected ref. No real kernel, no socket, no `restart_kernel`.
   - **Acceptance (live kernel):** using the existing `scripts/lib/kernel-harness.sh`, actually `workspace.allocate` → `workspace.release{policy=preserve}` → `restart_kernel` (kills and restarts the kernel + all plugin processes) → `workspace.get{work_context_id}` over the real socket → the released workspace comes back. This is the one that matters for M1.9; the Go test alone does not exercise the kernel/plugin restart path.

## 3. Components

### 3.1 `plugins/workspace/store.go` — `GetByContext`

```go
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

// refLess reports whether b sorts before a in "best" order — i.e. whether b
// should replace a as the current best. AllocatedAt/ReleasedAt descending,
// ID ascending; ID is a pure stable tiebreak with no time meaning.
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

`GetActiveByContext` is untouched, both in body and in its existing test.

### 3.2 `plugins/workspace/handlers.go` — `getHandler`

Change only the `work_context_id` branch:

```go
} else {
    ref, ok = s.GetByContext(q.WorkContextID)
}
```

`GetByID` (the `workspace_id` branch) is untouched. Error shape on no-candidate is unchanged: `{Code: "NOT_FOUND", Message: ErrNotFound.Error()}`.

### 3.3 `contracts/workspace.get/v1/schema.json` — documentation only

Add a top-level `"description"` string (sibling of `"contract"`/`"version"`/`"kind"`) stating that a `work_context_id` lookup may return a `RELEASED` workspace and callers must check `status` before assuming it is writable. **Nothing else in the file changes**: same `contract`, same `version` (`1.0.0`), same `request`/`response` shape, same `additionalProperties`/`required`. This is doc metadata, not a schema-semantics change — `check-contracts.py` validates `contract`/`version`/`kind`/request+response-JSON-Schema-validity only and does not enumerate or reject unknown top-level keys, so this is safe and contract count stays 31.

## 4. Tests (all required; all deterministic, no network/credentials)

**Go unit tests, `plugins/workspace/store_test.go` (new cases; existing cases unchanged):**

| test | setup | expect |
|---|---|---|
| multiple ALLOCATED | two `WorkspaceRef`s, same wc, both `ALLOCATED`, different `AllocatedAt` | `GetByContext` returns the one with the later `AllocatedAt` |
| only preserve-released | no ALLOCATED for the wc; two `RELEASED`+`preserve` refs, different `AllocatedAt` | returns the later `AllocatedAt` one |
| delete+preserve mixed | one `RELEASED`+`delete`, one `RELEASED`+`preserve` for the same wc | returns the `preserve` one; the `delete` one is never a candidate |
| equal `AllocatedAt` | two refs, identical `AllocatedAt`, different `ReleasedAt` | returns the one with the later `ReleasedAt` |
| equal `AllocatedAt` and `ReleasedAt` (or both `""`) | two refs, identical `AllocatedAt` and `ReleasedAt` | returns the one with the lexicographically **smaller** `ID` ("ID ascending" — `refLess` converges to the minimum) — assert the exact winner by ID, not just "a deterministic pick" |
| no candidates | wc has only a `RELEASED`+`delete` ref, or no ref at all | `GetByContext` returns `ok == false` |
| `GetActiveByContext` unaffected | same fixtures as `TestReleaseUpdatesStatusAndSurvivesReload` | existing assertion (`GetActiveByContext` on a released wc → not ok) still passes unchanged |

**Go persistence test (layer 1 of invariant 5):** allocate, release(preserve), `Load()` the directory into a fresh `Store`, `GetByContext` on the reloaded store returns the same ref (mirrors `TestReleaseUpdatesStatusAndSurvivesReload`'s existing reload pattern, extended to assert `GetByContext` too — not a replacement for it).

**Handler test, `plugins/workspace/handlers_test.go`:** `getHandler` with `work_context_id` after a `release(preserve)` returns the workspace with `Status == "RELEASED"` (not `NOT_FOUND`); after `release(delete)` (and no other workspace for that wc) returns `NOT_FOUND`.

**Acceptance test (layer 2 of invariant 5), `scripts/smoke-workspace.sh` (extend the existing script — no new script):** as `local-cli`, `workspace allocate` → `workspace release <id> -policy preserve` → `restart_kernel` → `.bin/vibe-raw -cap workspace.get -kind query ... -payload '{"work_context_id":"<wc>"}'` → response `workspace.status == "RELEASED"` and `workspace.id` equals the released id. Capture output into a variable, match with `case` (project shell-hygiene convention).

## 5. 致残 sweep

| # | mutation | expect |
|---|---|---|
| M1 | `GetByContext`: drop the `preserve`-only filter (also match `delete`) | new store test "delete+preserve mixed" fails (would sometimes return the deleted one) |
| M2 | `GetByContext`: change candidate selection to always prefer RELEASED over ALLOCATED | "multiple ALLOCATED" test fails |
| M3 | `refLess`: drop the `ID` tiebreak (final `return false`, so `best` never changes past `candidates[0]` on a full tie) | "equal AllocatedAt and ReleasedAt" test fails: on a full tie, `best` is whichever ref the `s.byID` map range happened to enumerate first — i.e. the result becomes map-iteration-order-dependent (flaky across runs), not the fixed minimum-`ID`. Asserting the *exact* winning ID (not just "a result") is what makes this bite. |
| M4 | `getHandler`: keep calling `GetActiveByContext` instead of `GetByContext` | handler test "returns RELEASED not NOT_FOUND" fails; acceptance test fails |

Each mutation applied, test run to confirm the exact expected failure, reverted, re-run to confirm green.

## 6. Acceptance (what "M1.8.5 done" means)

1. All new + existing `plugins/workspace` tests pass; `go test -race ./plugins/workspace/` clean.
2. `scripts/smoke-workspace.sh` (extended) passes, demonstrating release → `restart_kernel` → find-by-`work_context_id`.
3. `bash scripts/check-arch.sh` unchanged output (`31 contracts`, `10 manifests`).
4. `bash scripts/smoke.sh` (full suite) still `M1 SMOKE: PASSED`, no orphans — the changed selection must not regress any other consumer (none currently call `workspace.get{work_context_id}` besides the smoke script and the future M1.9 harness, but the full smoke run is the standing regression gate).
5. 致残 sweep M1–M4 each reproduce the expected failure, then restore green.
6. `git diff <base> HEAD` is exactly: `plugins/workspace/store.go`, `plugins/workspace/store_test.go`, `plugins/workspace/handlers.go`, `plugins/workspace/handlers_test.go`, `scripts/smoke-workspace.sh`, `contracts/workspace.get/v1/schema.json`, plus this spec/plan/dispatch docs.
7. Reviewer independently re-runs 1–5 (校验者≠生产者).

## 7. NON-GOALS

- A `workspace.query` capability (list all workspaces for a context, full history) — not needed by M1.9 or the Console v1 read-projection; would be a new contract.
- Any lock / single-writer-per-worktree semantics — unrelated to this gap; a future concern for parallel agents (M2+), not this milestone.
- Mirroring an `active_workspace_ref` into `work-registry` — rejected: workspace identity/lifecycle stays owned and queried by the workspace plugin, not duplicated into another plugin's state.
- Changing `workspace.allocate` / `workspace.release` behavior — untouched.
- M1.9's own changes (dropping the `workspace_id` bridge, re-running the read-projection acceptance, flipping ADR-002 to unconditional) — that is M1.9's work, gated on this milestone's acceptance, not part of this spec.

## 8. Drift guardrails

- **Design invariant:** no kernel/contract/schema-shape change; `GetActiveByContext` untouched; selection order is exactly ALLOCATED-priority → `AllocatedAt` desc → `ReleasedAt` desc → `ID` asc; no `CONFLICT` error; no map-order dependence.
- **Field ownership:** workspace lifecycle/identity is owned solely by the workspace plugin's store; no other plugin's state is read or written by this change.
- **Not inheritable from any other milestone:** do not add a lock, do not add `workspace.query`, do not touch `work-registry`.
- **Admission:** freezable — the selection rule, tiebreak, error semantics, and two-layer restart verification are all fixed above; the implementer's task is TDD execution of §3–§6, not further design.
