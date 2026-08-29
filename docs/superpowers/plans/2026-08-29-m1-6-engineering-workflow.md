# M1.6 — Engineering Workflow (composition) — Implementation Plan

**Execution:** work task-by-task, in order. Each task: write the failing test first, run it and watch it fail for the right reason, write the minimal implementation, run it green, then (for behavioural changes) briefly invert the production change to confirm the test goes red and restore it. Commit at the end of each task with the message given.

**Goal:** Land `org.vibe.workflow.engineering` — the **composition** plugin that drives the whole locked chain (`docs/M1-DESIGN.md` §3) as one synchronous call: create-time task → allocate worktree → run the agent → collect diff → build → test → attach evidence → request review → **wait (polling) for the human decision** → evaluate the **DONE gate** (§4.3) → transition the Task → seal the session → release the worktree. It owns **no canonical state**; every step is a contract call and a canonical journal event. This milestone also makes `check_composition.py` meaningfully constrain a real composition plugin (FIX-4), and restructures the M1 policy so the qualification identity can only reach `work.transition` **through** the workflow's delegation scope (§4.2) — the setup M1.7 attacks.

**Architecture:** A stateful-authority plugin (it needs a fence lease to serialise its own progress-event appends to `event.journal`, but it stores nothing private). `workflow.engineering.run@1` is a synchronous long-deadline command (CLI default 30 min). Orchestration lives in a `runPipeline(ctx, caps, req)` function over an injected `caps` struct of capability-call closures — fully unit-testable with fakes. WAITING_REVIEW is a bounded polling loop over `review.get`. `workflow.engineering.get@1` is a query that replays `event.journal` and projects pipeline progress by `correlation_id` (= `work_context_id`) — derived, not stored.

**Tech Stack:** Go standard library + `kernel/sdk/go/{protocol,pluginhost,fencing}` (via `go.work`, **no new external Go dependency**), newline-delimited JSON over a Unix socket, Python 3 + `jsonschema`.

**Spec:** `docs/M1-DESIGN.md` — §2 G1/G3/G4, §3 (the locked chain), §4.2 (policy + delegation — no `work.complete`), §4.3 (DONE gate conjunction), §4.4 (what M1.7 will attack — set it up here), §5.3 (`org.vibe.workflow.engineering` row: `composition: true` in manifest, no private state, wide `consumes`), §7 (stateless synchronous orchestration; WAITING_REVIEW = poll `review.get`; client disconnect ≠ cancel; CLI 30 min default), §9 (canonical event list), §13 milestone M1.6. Also review finding **A1** (`check_composition.py` must bind a real composition plugin), and `docs/ADR-002-human-console-interaction-model.md` (the confirmed Human Console model — its only M1 requirement is the additive `work.query@1` in Task 2A).

**Base:** branch `chatgpt/m1-6-engineering-workflow` from `main` at **`02e1014`** (filled at dispatch time). Present at that point: everything through M1.5 — 9 plugins wired (`blob`, `event-journal`, `work-registry`, `workspace`, `agent-harness`, `artifact`, `tool-runner`, `review`, `session`), `cli/vibe` with `task`/`workspace`/`agent`/`artifact`/`tool`/`review`/`session` subcommands, `scripts/smoke*.sh` (with `kill_kernel_tree` + query-readiness probe in `restart_kernel`), `config/m1-{policy,bindings}.json` with **29 contracts**, `architecture-tests/check_composition.py` (seeded, never bound to a real composition plugin).

## Global Constraints

- **G1 Kernel Purity:** no task modifies `kernel/` source. G1 check: `git diff --name-only 02e1014 HEAD -- kernel/internal kernel/cmd kernel/sdk` must be empty. If a step seems to need a kernel change, stop and report.
- **Do NOT touch `docs/M1-DESIGN.md`.** Not staged, not edited, not committed. §13 is the reviewer's post-merge step.
- **No new external Go modules.**
- **Module paths:** kernel `github.com/example/agent-native-microkernel`; plugins `github.com/example/agent-native-os/plugins`; CLI `github.com/example/agent-native-os/cli`.
- **Manifest rule:** export/consume `contract` field == `<capability>@<major>` exactly. The workflow plugin's `exports` entry is a stateful `command` (it needs a fence lease for its event appends); it has `runtime.data_namespace` for the lease dir only. It also has the manifest-level key `"composition": true`.
- **Contract rule:** `contracts/<dotted.name>/v<major>/schema.json`, register in `contracts/catalog.json`, then `python3 scripts/check-contracts.py --root contracts`.
- **Git identity in tests/scripts:** every `git commit` passes `-c user.email=test@example.com -c user.name=test` inline; a worktree needs at least one commit in the source repo.
- **Commit trailer** — every commit message ends with exactly:
  ```
  Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
  Plan: docs/superpowers/plans/2026-08-29-m1-6-engineering-workflow.md
  ```
- **Commit identity:** author `ada <oashasu@gmail.com>` (connector may substitute — known limit).
- **Number discipline:** any count in an expected-output string is whatever the plan's own commands produce; if a literal here differs, trust the commands, adjust the assertion, note it.
- **`go build ./cli/...` may drop a `./vibe` binary in cwd** — delete it, do not commit (`.gitignore` already has `/vibe`).

---

## File Structure

New:

- `contracts/workflow.engineering.run/v1/schema.json`, `contracts/workflow.engineering.get/v1/schema.json`
- `contracts/work.query/v1/schema.json` — ADR-002 console context enumeration (Task 2A)
- `plugins/engineering-workflow/pipeline.go`, `plugins/engineering-workflow/pipeline_test.go` — `runPipeline` + `caps` + `RunRequest`/`RunResult`
- `plugins/engineering-workflow/gate.go`, `plugins/engineering-workflow/gate_test.go` — pure DONE-gate evaluator
- `plugins/engineering-workflow/progress.go`, `plugins/engineering-workflow/progress_test.go` — journal → pipeline-progress projection
- `plugins/engineering-workflow/handlers.go`, `plugins/engineering-workflow/handlers_test.go` — the two capability handlers + the `caps` wiring over `rc`
- `plugins/engineering-workflow/main.go`
- `plugins/manifests/engineering-workflow.manifest.json`
- `scripts/smoke-workflow.sh`

Modified:

- `contracts/catalog.json` — +3 entries (2 workflow + `work.query@1`)
- `plugins/work-registry/{store.go,handlers.go,*_test.go}` + `plugins/manifests/work-registry.manifest.json` — additive `work.query@1` read query (Task 2A); no state-machine / authority / storage-shape change
- `architecture-tests/check_composition.py` — composition plugins exempt from the fan-in ceiling **but** must declare no `data_namespace`-backed private store beyond a fence lease and no stateless private authority; see Task 1
- `architecture-tests/thresholds.json` — unchanged values, doc note
- `config/m1-policy.json` — **restructure** (see Task 7): `local-cli` becomes the qualification identity (no direct `work.transition@1`, no direct `agent.run@1`, etc.); a `workflow.engineering.run@1` delegation scope is added; a new `m1-dev` client keeps the broad direct grants for the per-plugin fragment smokes; `org.vibe.workflow.engineering` grant lists every capability it consumes
- `config/m1-bindings.json` — bindings for `workflow.engineering.run` / `workflow.engineering.get` / `work.query`
- `cli/vibe/main.go` — `workflow` subcommand (`run` / `show`) + `work list` (Task 2A); the per-plugin fragment smokes' `vibe` calls that need `work.transition` / `agent.run` / `tool.run` / `artifact.collect_diff` etc. switch to `-identity m1-dev`
- `scripts/smoke-{workspace,agent,artifact,review-session}.sh` — use `-identity m1-dev` for the "doing" calls (they were written against a permissive `local-cli`)
- `scripts/smoke.sh` — export `DEV_TOKEN` alongside `TOKEN`; `source scripts/smoke-workflow.sh` last

---

## Task 1: bind `check_composition.py` to real composition plugins (FIX-4)

**Files:** `architecture-tests/check_composition.py` (modify), `architecture-tests/thresholds.json` (doc note).

The seeded check fails any plugin with `consumes >= consume_fail` (12) — with no exemption for composition plugins. The workflow consumes ~14. Fix: a plugin with `"composition": true` is **exempt from the fan-in ceiling** (fan-out is its job) but is checked **harder** on the state rule, and the fan-in is still surfaced as info.

- [ ] **Step 1: write the failing test**

The check has no Go test; drive it with fixture manifests. Add to `architecture-tests/` a small runner `check_composition_test.py` (or just do it inline in the task): create two temp manifest files and assert the checker's exit code + output.

Concretely, add fixtures under a temp dir the checker can be pointed at — but the current checker hard-codes `plugins/manifests`. Simplest: make the checker accept `--manifests <dir>` (default `plugins/manifests`), then the test points it at fixtures.

`check_composition_test.py`:

```python
import subprocess, sys, json, tempfile, os, pathlib

ROOT = pathlib.Path(__file__).resolve().parents[1]

def run(manifests_dir):
    return subprocess.run([sys.executable, str(ROOT / "architecture-tests" / "check_composition.py"),
                           "--manifests", str(manifests_dir)], capture_output=True, text=True)

def write(d, name, obj):
    (pathlib.Path(d) / name).write_text(json.dumps(obj))

def test_composition_plugin_allowed_high_fanin():
    with tempfile.TemporaryDirectory() as d:
        write(d, "wf.manifest.json", {
            "manifest_version": 1, "plugin": {"id": "org.vibe.wf", "version": "1"},
            "runtime": {"protocol": "vibe-plugin/1", "executable": "../bin/wf"},
            "composition": True,
            "exports": [{"capability": "wf.run", "major": 1, "contract": "wf.run@1", "mode": "stateless"}],
            "consumes": {"required": [{"capability": f"c{i}", "major": 1, "contract": f"c{i}@1"} for i in range(18)]},
        })
        r = run(d)
        assert r.returncode == 0, r.stdout + r.stderr

def test_composition_plugin_rejected_when_it_owns_state():
    with tempfile.TemporaryDirectory() as d:
        write(d, "bad.manifest.json", {
            "manifest_version": 1, "plugin": {"id": "org.vibe.bad", "version": "1"},
            "runtime": {"protocol": "vibe-plugin/1", "executable": "../bin/bad", "data_namespace": "state-authority/bad-main"},
            "composition": True,
            "exports": [{"capability": "bad.x", "major": 1, "contract": "bad.x@1", "mode": "stateful",
                         "service": "s", "authority": "bad-main"}],
        })
        r = run(d)
        assert r.returncode != 0 and "stateful" in r.stdout

def test_non_composition_still_capped():
    with tempfile.TemporaryDirectory() as d:
        write(d, "fat.manifest.json", {
            "manifest_version": 1, "plugin": {"id": "org.vibe.fat", "version": "1"},
            "runtime": {"protocol": "vibe-plugin/1", "executable": "../bin/fat"},
            "consumes": {"required": [{"capability": f"c{i}", "major": 1, "contract": f"c{i}@1"} for i in range(14)]},
        })
        r = run(d)
        assert r.returncode != 0 and "fail threshold" in r.stdout

if __name__ == "__main__":
    for name, fn in sorted(globals().items()):
        if name.startswith("test_"):
            fn(); print("ok", name)
```

- [ ] **Step 2: run it to verify failure** — `python3 architecture-tests/check_composition_test.py` → the composition-high-fanin case fails (checker rejects it) and/or `--manifests` is unknown.

- [ ] **Step 3: implement the checker changes.**
- Add `argparse` `--manifests` (default `plugins/manifests`), `--root` stays for `thresholds.json`.
- For each manifest: `is_comp = m.get("composition") is True`; `n = len(required)+len(optional)`.
  - `is_comp`:
    - **fan-in ceiling does not apply** — print `info: <id> is a composition plugin, consumes {n} capabilities` and do not error on `n >= consume_fail`.
    - **must own no canonical state**: any `exports[*].mode == "stateful"` → error `"{id}: composition plugin must not own a stateful export"`. (The workflow's own manifest uses a stateful *command* export for its fence lease — see the note below; resolve by the rule that the state check looks at whether the plugin backs a **domain authority**. Simplest robust rule for M1: a composition plugin's stateful exports are allowed **only** if the authority name ends with `-orchestration` AND there is exactly one such export. That lets the workflow keep a fence lease for its own event ordering without owning a domain authority. State it in the error message.)
  - not `is_comp`: unchanged (`n >= consume_fail` → error; `n >= consume_warn` → warn).
- Keep the final `COMPOSITION FITNESS: PASSED (<k> manifests)` line.

  > **Design note for the implementer:** the workflow needs to append to `event.journal` under a fence so its progress events for one run are ordered. It declares one stateful command export on authority `workflow-orchestration-main` with an empty/lease-only `data_namespace`. That is *ordering state*, not *canonical domain state* — the check allows exactly this shape and nothing more.

- [ ] **Step 4: run the test green.** Also run `python3 architecture-tests/check_composition.py` against the real `plugins/manifests` → still `PASSED` (no composition plugin exists yet).

- [ ] **Step 5: mutation check** — remove the `is_comp` exemption (composition plugins hit the ceiling again). `test_composition_plugin_allowed_high_fanin` → FAIL. Restore.

- [ ] **Step 6: commit** — `feat(m1.6): check_composition binds real composition plugins (FIX-4)`

---

## Task 2: workflow contracts

**Files:** two schemas; modify `contracts/catalog.json`.

`RunRequest` = the `workflow.engineering.run@1` request. `RunResult` shape:

```
RunResult = {
  work_context_id, task_id,
  outcome:   "DONE" | "GATE_FAILED" | "AGENT_FAILED" | "BUILD_FAILED" | "TEST_FAILED" | "TIMEOUT",
  reason,                       # populated when outcome != DONE
  agent_run_id, diff_artifact_id, build_tool_run_id, test_tool_run_id, review_id, session_id,
  event_ids: [string]          # every canonical event this run appended
}
```

- [ ] **Step 1: `contracts/workflow.engineering.run/v1/schema.json`** — `kind` `"command"`, request:

```json
  "request": {
    "type": "object",
    "additionalProperties": false,
    "properties": {
      "task_id": { "type": "string" },
      "prompt": { "type": "string" },
      "build_command": { "type": "array", "items": { "type": "string" }, "minItems": 1 },
      "test_command": { "type": "array", "items": { "type": "string" }, "minItems": 1 },
      "base_ref": { "type": "string" },
      "review_poll_ms": { "type": "integer" },
      "mock_agent_write_file": { "type": "string" },
      "mock_agent_write_content": { "type": "string" }
    },
    "required": ["task_id", "prompt", "build_command", "test_command"]
  }
```

response `{ "result": { "type": "object" } }` required. (`mock_agent_*` are forwarded to `agent.run` so the smoke can make the mock touch a real file; the real provider ignores them in M1.8.)

- [ ] **Step 2: `contracts/workflow.engineering.get/v1/schema.json`** — `kind` `"query"`, request `{ "work_context_id": string }` required (or `{ "task_id": string }` — accept either, exactly one). response:

```json
  "response": {
    "type": "object",
    "additionalProperties": true,
    "properties": {
      "stage": { "type": "string" },
      "events": { "type": "array" },
      "outcome": { "type": "string" }
    },
    "required": ["stage", "events"]
  }
```

- [ ] **Step 3: catalog + check** — add `workflow.engineering.run@1` / `workflow.engineering.get@1`. `python3 scripts/check-contracts.py --root contracts` → PASSED (29 + 2 = 31 here; Task 2A adds `work.query@1` for a final 32 — trust the command's own count).

- [ ] **Step 4: commit** — `build(m1.6): workflow.engineering.run/get contracts`

---

## Task 2A: `work.query@1` + `vibe work list` (ADR-002)

The Console's context switcher — and headless `vibe work list` — must enumerate every
WorkContext with its status. work-registry today only answers `work.get(task_id)`. This
is an **additive read query**: no state-machine, authority, or storage-shape change. `§10`
single-task qualification does not depend on it; it lands here because M1.6 already touches
work-registry's contract set, the M1 bindings, and the M1 policy (Task 7).

**Files:**
- Create: `contracts/work.query/v1/schema.json`
- Modify: `contracts/catalog.json` (+1 entry)
- Modify: `plugins/work-registry/store.go` — add `List(statusFilter string) []WorkContextRow` over the in-memory projection (stable order by creation)
- Modify: `plugins/work-registry/handlers.go` — add the `work.query` handler
- Modify: `plugins/work-registry/handlers_test.go` (or the store test file) — the failing test below
- Modify: `plugins/manifests/work-registry.manifest.json` — add the `work.query@1` **query** export (no new authority; same `work-main`)
- Modify: `cli/vibe/main.go` — `vibe work list [-status X]` subcommand
- Modify: `config/m1-bindings.json` — bind `work.query` (Task 8 also touches this file — either task may add it; if Task 8 runs first, skip here)
- Note: `config/m1-policy.json` gets `work.query@1` in the `local-cli` and `m1-dev` capability lists — already written into the Task 7 JSON.

**Contract** `contracts/work.query/v1/schema.json` — `kind` `"query"`:

```json
{
  "capability": "work.query",
  "major": 1,
  "kind": "query",
  "request": {
    "type": "object",
    "additionalProperties": false,
    "properties": {
      "status": { "type": "string", "enum": ["PLANNED", "IN_PROGRESS", "IN_REVIEW", "DONE", "FAILED"] },
      "limit":  { "type": "integer", "minimum": 1 },
      "after":  { "type": "string" }
    }
  },
  "response": {
    "type": "object",
    "additionalProperties": false,
    "properties": {
      "work_contexts": {
        "type": "array",
        "items": {
          "type": "object",
          "additionalProperties": true,
          "properties": {
            "work_context_id": { "type": "string" },
            "task_id":         { "type": "string" },
            "title":           { "type": "string" },
            "status":          { "type": "string" },
            "repo":            { "type": "string" },
            "version":         { "type": "integer" }
          },
          "required": ["work_context_id", "task_id", "title", "status", "repo", "version"]
        }
      },
      "next": { "type": ["string", "null"] }
    },
    "required": ["work_contexts", "next"]
  }
}
```

`status` is work-registry's own vocabulary (already in the Task state machine), so it does **not** trip G1. No engineering-semantic filter beyond it.

- [ ] **Step 1: failing test** — in the work-registry test package: create three tasks; transition one to `IN_PROGRESS`, one to `IN_REVIEW`; call `work.query` with no filter → all three, ordered by creation; with `status=IN_PROGRESS` → exactly one, the right one; each row carries `work_context_id/task_id/title/status/repo/version`.

```go
func TestWorkQuery_FilterAndOrder(t *testing.T) {
	st := newTestStore(t)
	a := mustCreate(t, st, "alpha")
	b := mustCreate(t, st, "beta")
	c := mustCreate(t, st, "gamma")
	mustTransition(t, st, b, "IN_PROGRESS")
	mustTransition(t, st, c, "IN_PROGRESS")
	mustTransition(t, st, c, "IN_REVIEW")

	all := st.List("")
	if len(all) != 3 || all[0].TaskID != a.TaskID || all[2].TaskID != c.TaskID {
		t.Fatalf("want alpha,beta,gamma in creation order, got %+v", all)
	}
	inprog := st.List("IN_PROGRESS")
	if len(inprog) != 1 || inprog[0].TaskID != b.TaskID {
		t.Fatalf("want only beta IN_PROGRESS, got %+v", inprog)
	}
}
```

- [ ] **Step 2: run — watch it fail** (`st.List` undefined). `go test ./plugins/work-registry/... -run TestWorkQuery`.
- [ ] **Step 3: implement** — `WorkContextRow` struct + `Store.List(statusFilter string) []WorkContextRow` (iterate the projection in insertion order, filter by `status` when non-empty); `work.query` handler decodes `{status,limit,after}`, calls `List`, applies `limit`/`after` (opaque cursor = last `work_context_id`; `next` is null when the page is not truncated); register `work.query@1` in `contracts/catalog.json`; add the manifest export.
- [ ] **Step 4: run — green.** Also `python3 scripts/check-contracts.py --root contracts` PASSED.
- [ ] **Step 5: mutation check** — make `List` ignore `statusFilter` (always return all); the `status=IN_PROGRESS` assertion goes red; restore.
- [ ] **Step 6: `vibe work list`** — `cli/vibe/main.go`: `vibe work list [-status PLANNED|IN_PROGRESS|IN_REVIEW|DONE|FAILED]` → `work.query@1` query; print one line per row: `<task_id>  <status>  <title>`. Add the `work.query` binding to `config/m1-bindings.json` if not already present.
- [ ] **Step 7: build + tests green** — `go build ./... && go test ./plugins/work-registry/... ./cli/...` (delete any stray `./vibe`).
- [ ] **Step 8: commit** — `feat(m1.6): work.query@1 + vibe work list (ADR-002 console context enumeration)`

---

## Task 3: `gate.go` — the DONE-gate evaluator

**Files:** `plugins/engineering-workflow/gate.go`, `plugins/engineering-workflow/gate_test.go`.

**Interfaces:**

```go
type EvidenceOutcome struct{ Outcome string } // "PASS" | "FAIL"
type ReviewState struct {
	Status            string   // "PENDING" | "APPROVED" | "CHANGES_REQUESTED"
	DiffArtifactID    string
	AcceptanceResults []struct{ Satisfied bool }
}

// doneGate returns (true, "") only when every §4.3 condition holds. Otherwise
// (false, "<first failing condition>").
func doneGate(build, test EvidenceOutcome, review ReviewState, currentDiffArtifactID string) (bool, string)
```

- [ ] **Step 1: failing test** (`gate_test.go`):

```go
package main

import "testing"

func approved(diff string, acc ...bool) ReviewState {
	r := ReviewState{Status: "APPROVED", DiffArtifactID: diff}
	for _, a := range acc {
		r.AcceptanceResults = append(r.AcceptanceResults, struct{ Satisfied bool }{a})
	}
	return r
}

func TestDoneGateAllGreen(t *testing.T) {
	ok, why := doneGate(EvidenceOutcome{"PASS"}, EvidenceOutcome{"PASS"}, approved("art-1", true, true), "art-1")
	if !ok {
		t.Fatalf("expected pass, got %q", why)
	}
}

func TestDoneGateFailsOnEachCondition(t *testing.T) {
	cases := []struct {
		name  string
		b, tt EvidenceOutcome
		rv    ReviewState
		diff  string
		wwant string
	}{
		{"build fail", EvidenceOutcome{"FAIL"}, EvidenceOutcome{"PASS"}, approved("a", true), "a", "build"},
		{"test fail", EvidenceOutcome{"PASS"}, EvidenceOutcome{"FAIL"}, approved("a", true), "a", "test"},
		{"not approved", EvidenceOutcome{"PASS"}, EvidenceOutcome{"PASS"}, ReviewState{Status: "CHANGES_REQUESTED", DiffArtifactID: "a"}, "a", "review"},
		{"wrong diff", EvidenceOutcome{"PASS"}, EvidenceOutcome{"PASS"}, approved("other", true), "a", "diff"},
		{"acceptance unsatisfied", EvidenceOutcome{"PASS"}, EvidenceOutcome{"PASS"}, approved("a", true, false), "a", "acceptance"},
	}
	for _, c := range cases {
		ok, why := doneGate(c.b, c.tt, c.rv, c.diff)
		if ok || !contains(why, c.want) {
			t.Fatalf("%s: ok=%v why=%q want substr %q", c.name, ok, why, c.want)
		}
	}
}
```

(add a tiny `contains` helper or use `strings.Contains`.)

- [ ] **Step 2: run to verify failure.**
- [ ] **Step 3: implement `gate.go`** — check in the §4.3 order; first failure wins; empty `AcceptanceResults` → treat as unsatisfied (`"acceptance: no results"`).
- [ ] **Step 4: run green.**
- [ ] **Step 5: mutation check** — drop the `review.DiffArtifactID == currentDiffArtifactID` check. `wrong diff` case → FAIL. Restore.
- [ ] **Step 6: commit** — `feat(m1.6): DONE-gate evaluator`

---

## Task 4: `pipeline.go` — the orchestration

**Files:** `plugins/engineering-workflow/pipeline.go`, `plugins/engineering-workflow/pipeline_test.go`.

**Interfaces:**

```go
type caps struct {
	WorkGet          func(taskID string) (task Task, wcID string, taskVersion int, err error)
	WorkTransition   func(wcID, to string, expectedVersion int) (newVersion int, err error)
	WorkspaceAlloc   func(wcID, baseRef string) (wsID, wsPath string, err error)
	WorkspaceRelease func(wsID, policy string) error
	AgentRun         func(wcID, wsPath, prompt, writeFile, writeContent string) (agentRunID, status string, err error) // blocks until terminal
	CollectDiff      func(wcID, wsPath string) (artifactID string, filesChanged int, err error)
	ToolRun          func(wcID, wsPath, label string, argv []string) (toolRunID, outcome string, err error)
	AttachEvidence   func(wcID, kind, srcCap, srcID, outcome string) error
	ReviewRequest    func(wcID, agentRunID, diffArtifactID string, snapshot []EvItem) (reviewID string, err error)
	ReviewGet        func(reviewID string) (ReviewState, error)
	SessionSeal      func(wcID, agentRunID, wsPath string) (sessionID string, err error)
	AppendEvent      func(eventType string, payload map[string]any) (eventID string, err error)
	Sleep            func(d time.Duration)
	Now              func() string
}
type EvItem struct{ Kind, Outcome, EvidenceRefID string }
type RunRequest struct {
	TaskID, Prompt, BaseRef      string
	BuildCommand, TestCommand    []string
	ReviewPollMS                 int
	MockAgentWriteFile, MockAgentWriteContent string
}
type RunResult struct {
	WorkContextID, TaskID string
	Outcome, Reason       string
	AgentRunID, DiffArtifactID, BuildToolRunID, TestToolRunID, ReviewID, SessionID string
	EventIDs              []string
}

func runPipeline(ctx context.Context, c caps, req RunRequest) RunResult
```

**Step sequence** (each step: do the call → `AppendEvent(<type>, {...})` → record the event id; on a hard error set `Outcome` + `Reason` and return early — but always attempt `WorkspaceRelease(policy=preserve)` and, if a diff+session make sense, still `SessionSeal` before returning so evidence is not lost):

1. `WorkGet(req.TaskID)` → task, wcID, ver. `AppendEvent("work.created" only if already exists? no)` — actually the task pre-exists; emit nothing here. Fail → `Outcome="GATE_FAILED"`, `Reason="task not found"`.
2. `WorkspaceAlloc(wcID, req.BaseRef)` → wsID, wsPath. `AppendEvent("workspace.allocated", {workspace_id, path})`.
3. `WorkTransition(wcID, "IN_PROGRESS", ver)` → ver=new. `AppendEvent("work.transitioned", {from:"PLANNED", to:"IN_PROGRESS"})`.
4. `AgentRun(...)` → agentRunID, status. `AppendEvent("agent.run.started", {agent_run_id})` before, `AppendEvent("agent.run.completed", {agent_run_id, status})` after. status != "COMPLETED" → `Outcome="AGENT_FAILED"`, seal + release, return.
5. `CollectDiff(wcID, wsPath)` → diffArtID, filesChanged. `AppendEvent("diff.collected", {artifact_id, files_changed})`.
6. `ToolRun(wcID, wsPath, "build", req.BuildCommand)` → buildTR, buildOutcome. `AppendEvent("tool.run.completed", {tool_run_id, label:"build", outcome})`. `AttachEvidence(wcID, "build", "tool.run@1", buildTR, buildOutcome)`; `AppendEvent("evidence.attached", {kind:"build", source_id:buildTR, outcome})`.
7. Same for `"test"` / `req.TestCommand`.
8. `WorkTransition(wcID, "IN_REVIEW", ver)` → ver=new. `AppendEvent("work.transitioned", {from:"IN_PROGRESS", to:"IN_REVIEW"})`.
9. `ReviewRequest(wcID, agentRunID, diffArtID, snapshot=[{build,buildOutcome},{test,testOutcome}])` → reviewID. `AppendEvent("review.requested", {review_id, diff_artifact_id})`.
10. `AppendEvent("workflow.waiting_review", {review_id})`. Then poll: `for { select ctx.Done → Outcome="TIMEOUT"; default } rs, _ := ReviewGet(reviewID); if rs.Status != "PENDING" break; Sleep(pollInterval)`. `pollInterval = req.ReviewPollMS ms` (default 500). `AppendEvent("review.decided", {review_id, status})`.
11. `ok, why := doneGate(build, test, rs, diffArtID)`. If `!ok` → `Outcome="GATE_FAILED"`, `Reason=why`, seal + release, return (Task stays IN_REVIEW).
12. `WorkTransition(wcID, "DONE", ver)`. `AppendEvent("work.transitioned", {from:"IN_REVIEW", to:"DONE"})`.
13. `SessionSeal(wcID, agentRunID, wsPath)` → sessionID. `AppendEvent("session.sealed", {session_id})`.
14. `WorkspaceRelease(wsID, "preserve")`. `AppendEvent("workspace.released", {workspace_id, policy:"preserve"})`.
15. `Outcome="DONE"`.

Every `AppendEvent` return id is appended to `RunResult.EventIDs`. `ctx.Done()` at any await point (`AgentRun`, the review poll) → `Outcome="TIMEOUT"` and best-effort seal + release.

**Client disconnect ≠ cancel:** `runPipeline` uses the passed `ctx` (the handler passes `rc.Context()` which is the request deadline, NOT the socket). Nothing in the pipeline watches the socket. State this in a comment.

- [ ] **Step 1: failing test** (`pipeline_test.go`) — a `fakeCaps` recording every call; drive `runPipeline` with a background `ctx` and:
  - **happy path:** agent COMPLETED, both tools PASS, `ReviewGet` returns PENDING twice then APPROVED with satisfied acceptance and matching diff → `Outcome=="DONE"`; assert the call order (allocate → transition IN_PROGRESS → agent → diff → build → test → transition IN_REVIEW → review.request → review poll → transition DONE → seal → release); assert `len(EventIDs) >= 12` and includes a `workflow.waiting_review`.
  - **gate fail on test:** test tool returns FAIL → `Outcome=="GATE_FAILED"`, `Reason` mentions "test", **no** `WorkTransition(_, "DONE", _)` call, but `SessionSeal` still called.
  - **agent failed:** `AgentRun` returns status "FAILED" → `Outcome=="AGENT_FAILED"`, no build/test calls, seal + release still attempted.
  - **review timeout:** `ReviewGet` always PENDING + a `ctx` with a 50 ms deadline → `Outcome=="TIMEOUT"`.

- [ ] **Step 2: run to verify failure.**
- [ ] **Step 3: implement `pipeline.go`.**
- [ ] **Step 4: run green.**
- [ ] **Step 5: mutation checks** — (a) in step 11, call `WorkTransition(DONE)` even when `!ok`. The "gate fail on test" test fails (a DONE transition happened). Restore. (b) remove the `workflow.waiting_review` `AppendEvent`. The happy-path test fails on the event assertion. Restore.
- [ ] **Step 6: commit** — `feat(m1.6): pipeline orchestration + WAITING_REVIEW polling`

---

## Task 5: `progress.go` — journal → progress projection

**Files:** `plugins/engineering-workflow/progress.go`, `plugins/engineering-workflow/progress_test.go`.

**Interfaces:**

```go
type JournalRecord struct {
	ID            string          `json:"id"`
	Type          string          `json:"type"`
	CorrelationID string          `json:"correlation_id"`
	Payload       json.RawMessage `json:"payload"`
}
// project filters records to correlationID and returns the furthest-reached stage
// plus the matching records in order.
func project(records []JournalRecord, correlationID string) (stage string, matched []JournalRecord)
```

`stage` is derived from the last matching event type: `workspace.allocated → "WORKSPACE"`, `agent.run.started → "AGENT"`, `diff.collected → "DIFF"`, `tool.run.completed{label:build} → "BUILD"`, `{label:test} → "TEST"`, `work.transitioned{to:IN_REVIEW} → "REVIEW_REQUESTED"`, `workflow.waiting_review → "WAITING_REVIEW"`, `review.decided → "REVIEWED"`, `work.transitioned{to:DONE} → "DONE"`, `session.sealed → "SEALED"`, else `"CREATED"`.

- [ ] **Step 1: failing test** — feed a mixed list (two correlation ids interleaved); `project(recs, "wc-1")` returns only wc-1 records, in order, and `stage == "WAITING_REVIEW"` when that is the last wc-1 event.
- [ ] **Step 2: run to verify failure.**
- [ ] **Step 3: implement.**
- [ ] **Step 4: run green.**
- [ ] **Step 5: mutation check** — make `project` ignore `correlationID` (return all). The interleaved test fails (wrong count / leaked events). Restore.
- [ ] **Step 6: commit** — `feat(m1.6): journal → pipeline-progress projection`

---

## Task 6: handlers + `caps` wiring + main.go + manifest

**Files:** `plugins/engineering-workflow/handlers.go`, `plugins/engineering-workflow/handlers_test.go`, `plugins/engineering-workflow/main.go`, `plugins/manifests/engineering-workflow.manifest.json`.

**Interfaces:** `runHandler(mkCaps func(rc *pluginhost.RequestContext, e protocol.Envelope) caps) pluginhost.ContextHandler`, `getHandler(replay func() ([]JournalRecord, error)) pluginhost.Handler`.

- The `caps` closures wrap `rc.Command` / `rc.Query` / `rc.CommandStream`. Specifically:
  - `AgentRun`: `rc.CommandStream("agent.run", 1, {work_context_id, workspace_path, prompt, provider:"mock", mock_write_file, mock_write_content}, timeout)` → drain the returned `*Stream.C` until `stream.close` → then `rc.Query("agent.run.get", 1, {agent_run_id})` for the terminal status. (`agent_run_id` comes from the accepted response payload.)
  - `AppendEvent`: `rc.Command("event.journal.append", 1, {type, source:"org.vibe.workflow.engineering", payload}, ...)`. The payload map includes `"work_context_id"` and the plugin sets the envelope's `correlation_id` = wcID via `rc.Command` — **but `rc.Command` builds the child envelope with the parent's correlation_id**. To force `correlation_id = wcID` on the journal record, put `work_context_id` in the payload and have `project` filter on `payload.work_context_id` rather than the record's `correlation_id`. **Simpler and more robust — use that.** (Update `progress.go` / `JournalRecord` to read `Payload.work_context_id`; adjust Task 5's test accordingly. Note this in the report as a refinement of the plan.)
- `runHandler`: parse `RunRequest`; validate (`task_id`, `prompt`, non-empty `build_command`/`test_command`); build `caps` from `rc`; `res := runPipeline(rc.Context(), c, req)`; return `{result: res}`. The plugin's own event appends are the only writes; wrap the *whole* pipeline call so a fence lease is held for its duration is NOT needed — each `AppendEvent` is a separate `rc.Command` to the journal, which fences itself. The workflow plugin's stateful export exists only so the kernel gives it a lease *identity*; it does not call `fencing.WithWriteFence` itself. (If `check_composition.py` or the kernel rejects a stateful export with no `fencing.WithWriteFence` usage — it will not; the kernel only checks the manifest shape.)

  Actually simpler: **make the workflow export `mode: "stateless"`.** It stores nothing and fences nothing (the journal plugin does the fencing for the appends). Then the manifest has no `data_namespace`, no authority, and `check_composition.py`'s "no stateful export" rule passes trivially. Drop the "-orchestration authority" special case from Task 1 — a composition plugin simply has **zero stateful exports**. Update Task 1 accordingly (remove the `-orchestration` carve-out; the rule is clean: `is_comp` ⇒ no `mode: "stateful"` export, period).

- [ ] **Step 1: failing test** (`handlers_test.go`) — `runHandler` with a `mkCaps` that returns a `fakeCaps` (happy path) and a hand-built command envelope → `{result: {outcome: "DONE", ...}}`; missing `task_id` → `INVALID`. `getHandler` with a fake `replay` returning interleaved records → `{stage: "...", events: [...]}` filtered by `work_context_id`.

- [ ] **Step 2: run to verify failure.**
- [ ] **Step 3: implement `handlers.go` + `main.go`.**
- [ ] **Step 4: `plugins/manifests/engineering-workflow.manifest.json`**

```json
{
  "manifest_version": 1,
  "plugin": { "id": "org.vibe.workflow.engineering", "version": "1.0.0" },
  "runtime": {
    "protocol": "vibe-plugin/1",
    "executable": "../bin/engineering-workflow",
    "isolation": "process"
  },
  "composition": true,
  "exports": [
    { "capability": "workflow.engineering.run", "major": 1, "contract": "workflow.engineering.run@1", "mode": "stateless", "priority": 100 },
    { "capability": "workflow.engineering.get", "major": 1, "contract": "workflow.engineering.get@1", "mode": "stateless", "priority": 100 }
  ],
  "consumes": {
    "required": [
      { "capability": "work.get", "major": 1, "contract": "work.get@1" },
      { "capability": "work.transition", "major": 1, "contract": "work.transition@1" },
      { "capability": "work.attach_evidence", "major": 1, "contract": "work.attach_evidence@1" },
      { "capability": "workspace.allocate", "major": 1, "contract": "workspace.allocate@1" },
      { "capability": "workspace.release", "major": 1, "contract": "workspace.release@1" },
      { "capability": "agent.run", "major": 1, "contract": "agent.run@1" },
      { "capability": "agent.run.get", "major": 1, "contract": "agent.run.get@1" },
      { "capability": "artifact.collect_diff", "major": 1, "contract": "artifact.collect_diff@1" },
      { "capability": "tool.run", "major": 1, "contract": "tool.run@1" },
      { "capability": "review.request", "major": 1, "contract": "review.request@1" },
      { "capability": "review.get", "major": 1, "contract": "review.get@1" },
      { "capability": "session.seal", "major": 1, "contract": "session.seal@1" },
      { "capability": "event.journal.append", "major": 1, "contract": "event.journal.append@1" },
      { "capability": "event.journal.replay", "major": 1, "contract": "event.journal.replay@1" }
    ]
  },
  "restart": { "mode": "on_failure", "max_attempts": 2, "cooldown_ms": 200 },
  "resources": { "memory_mb": 256, "cpu_weight": 30 }
}
```

- [ ] **Step 5: build + composition** — `bash scripts/build.sh` → `built plugin: engineering-workflow`; `python3 architecture-tests/check_composition.py` → `PASSED` with an `info:` line noting the workflow consumes 14 (over the ceiling, exempt as composition).

- [ ] **Step 6: commit** — `feat(m1.6): engineering-workflow handlers + plugin wiring`

---

## Task 7: policy restructure

**Files:** `config/m1-policy.json`.

Split the single permissive `local-cli` into: a **qualification identity** (`local-cli`) that can only *start a workflow* and *decide a review* and *read things*, and a **dev identity** (`m1-dev`) that keeps broad direct grants for the per-plugin fragment smokes.

- [ ] **Step 1: rewrite `config/m1-policy.json`**

```json
{
  "clients": {
    "local-cli": { "token_sha256": "<sha256 of 'm1-local-cli-token'>" },
    "m1-dev":    { "token_sha256": "<sha256 of 'm1-dev-token'>" }
  },
  "grants": {
    "local-cli": {
      "capabilities": [
        "work.create@1", "work.get@1", "work.query@1",
        "workflow.engineering.run@1", "workflow.engineering.get@1",
        "review.request@1", "review.decide@1", "review.get@1", "review.query@1",
        "agent.run.get@1", "agent.run.query@1",
        "artifact.get@1", "artifact.query@1",
        "tool.run.get@1", "tool.run.query@1",
        "workspace.get@1", "session.get@1", "session.query@1",
        "blob.get@1", "blob.stat@1",
        "event.journal.replay@1"
      ],
      "delegations": {
        "workflow.engineering.run@1": [
          "work.get@1", "work.transition@1",
          "workspace.allocate@1", "workspace.release@1",
          "agent.run@1", "agent.run.get@1",
          "artifact.collect_diff@1",
          "tool.run@1",
          "work.attach_evidence@1",
          "review.request@1", "review.get@1",
          "session.seal@1",
          "event.journal.append@1", "event.journal.replay@1",
          "blob.put@1"
        ]
      }
    },
    "m1-dev": {
      "capabilities": [
        "event.journal.append@1", "event.journal.replay@1",
        "blob.put@1", "blob.get@1", "blob.stat@1",
        "work.create@1", "work.get@1", "work.query@1", "work.transition@1", "work.attach_evidence@1",
        "workspace.allocate@1", "workspace.release@1", "workspace.get@1",
        "agent.run@1", "agent.run.get@1", "agent.run.query@1", "agent.run.cancel@1",
        "artifact.collect_diff@1", "artifact.get@1", "artifact.query@1",
        "tool.run@1", "tool.run.get@1", "tool.run.query@1",
        "review.request@1", "review.decide@1", "review.get@1", "review.query@1",
        "session.seal@1", "session.get@1", "session.query@1",
        "workflow.engineering.run@1", "workflow.engineering.get@1"
      ]
    },
    "org.vibe.event.journal": { "capabilities": [] },
    "org.vibe.blob": { "capabilities": [] },
    "org.vibe.work.registry": { "capabilities": [] },
    "org.vibe.workspace": { "capabilities": [] },
    "org.vibe.agent.harness": { "capabilities": ["blob.put@1"], "service_authority": true },
    "org.vibe.artifact": { "capabilities": ["blob.put@1"] },
    "org.vibe.tool.runner": { "capabilities": ["blob.put@1"] },
    "org.vibe.review": { "capabilities": [] },
    "org.vibe.session": { "capabilities": ["event.journal.replay@1", "blob.put@1"] },
    "org.vibe.workflow.engineering": {
      "capabilities": [
        "work.get@1", "work.transition@1", "work.attach_evidence@1",
        "workspace.allocate@1", "workspace.release@1",
        "agent.run@1", "agent.run.get@1",
        "artifact.collect_diff@1",
        "tool.run@1",
        "review.request@1", "review.get@1",
        "session.seal@1",
        "event.journal.append@1", "event.journal.replay@1"
      ]
    }
  }
}
```

Compute the two `token_sha256` values with `printf '%s' '<token>' | shasum -a 256 | awk '{print $1}'` and paste them in. Keep the `local-cli` hash equal to the existing one (token `m1-local-cli-token` is unchanged).

- [ ] **Step 2: sanity** — `python3 -c "import json; json.load(open('config/m1-policy.json'))"` (valid JSON); no test yet — the smoke in Task 9 exercises it.

- [ ] **Step 3: commit** — `feat(m1.6): policy restructure — local-cli is the qualification identity, m1-dev keeps broad grants`

---

## Task 8: bindings + `vibe workflow` + fragment smokes use `m1-dev`

**Files:** `config/m1-bindings.json`, `cli/vibe/main.go`, `scripts/smoke-{workspace,agent,artifact,review-session}.sh`, `scripts/smoke.sh`.

- [ ] **Step 1: bindings** — add `workflow.engineering.run` / `workflow.engineering.get` (no service/authority — the workflow's exports are stateless, so bindings only need `{capability, major}`; match how other stateless caps are bound, or omit if the kernel does not require a binding for stateless). Also add the `work.query` binding if Task 2A did not already add it (match the existing `work.get` binding — same `work-main` authority).

- [ ] **Step 2: `vibe workflow` subcommand** in `cli/vibe/main.go`
- `vibe workflow run <task-id> -prompt "<text>" [-build "<argv...>"] [-test "<argv...>"] [-review-poll-ms N] [-mock-write-file <rel>] [-mock-write-content <s>] [-timeout <dur, default 30m>]` → an `invoke` with a `workflow.engineering.run@1` command; set the envelope `Deadline` to `now + timeout` (default 30 min); on return print `outcome <outcome>  task <task_id>  reason <reason>` and the key ids. `-build` / `-test` are space-split into argv (default `["sh","-c","true"]` for both).
- `vibe workflow show <task-id-or-wc-id>` → `workflow.engineering.get@1`; print `stage <stage>` and one line per event `<type>`.

- [ ] **Step 3: point the fragment smokes at `m1-dev`** — in `scripts/smoke-workspace.sh`, `scripts/smoke-agent.sh`, `scripts/smoke-artifact.sh`, `scripts/smoke-review-session.sh`, the `$V` / `$RAW` definitions currently use `-identity local-cli`. Change the ones that perform "doing" calls (`task create`, `workspace allocate`, `agent run`, `artifact collect-diff`, `tool run`, `review request`, raw `event.journal.append`, raw `blob.get`) to `-identity m1-dev -token $DEV_TOKEN`. Read-only `*.get` calls may stay on `local-cli`. Simplest: define `V="$V_DEV"` at the top of each fragment. (`review decide` may stay on `local-cli` since the qualification identity keeps `review.decide@1`.)

- [ ] **Step 4: `scripts/smoke.sh`** — add `DEV_TOKEN='m1-dev-token'` and `export DEV_TOKEN VIBE_DEV=...`; `export` it; `source scripts/smoke-workflow.sh` as the last fragment.

- [ ] **Step 5: build + all fragment smokes still green** — `bash scripts/smoke.sh` → every existing fragment (`WORKSPACE`, `AGENT`, `ARTIFACT+TOOL`, `REVIEW+SESSION`) still `OK`. This proves the policy split did not break the earlier milestones.

- [ ] **Step 6: commit** — `feat(m1.6): bindings + vibe workflow subcommand + fragment smokes use m1-dev`

---

## Task 9: `smoke-workflow.sh` — full chain end to end

**Files:** `scripts/smoke-workflow.sh`; `scripts/smoke.sh` already sources it (Task 8).

```bash
#!/usr/bin/env bash
# M1.6 smoke: the whole chain via one `workflow run`, with a human review decision
# from a second "terminal", ending in a DONE Task + a SessionRecord. Also asserts
# the qualification identity CANNOT transition a Task directly (M1.7 preview).
set -euo pipefail
VD=".bin/vibe -socket $SOCK -identity m1-dev -token $DEV_TOKEN"
VQ=".bin/vibe -socket $SOCK -identity local-cli -token $TOKEN"

SRC="$DATA/wfsrc"; mkdir -p "$SRC"
git -C "$SRC" -c init.defaultBranch=main init -q
printf 'class Calc { int add(int a,int b){return a+b;} }\n' > "$SRC/Calc.java"
git -C "$SRC" add -A
git -C "$SRC" -c user.email=s@t -c user.name=s -c commit.gpgsign=false commit -q -m init

TASK_ID="$($VD task create -title "wf" -goal "harden add" -repo "$SRC" -ac AC1="build+test pass" | sed -n 's/^task \([^ ]*\).*/\1/p')"

# qualification identity cannot transition directly (M1.7 preview)
$VQ task transition "$TASK_ID" -to IN_PROGRESS -expected-version 1 2>&1 | grep -qi 'denied\|did not grant\|error' \
  || { echo "FAIL: local-cli was able to call work.transition directly"; exit 1; }

# run the workflow in the background; it will block at WAITING_REVIEW
( $VQ workflow run "$TASK_ID" -prompt "harden add" -build "sh -c true" -test "sh -c true" \
    -review-poll-ms 200 -mock-write-file Calc.java -mock-write-content '// hardened
' -timeout 3m > "$DATA/wf.out" 2>&1 ) &
WF_PID=$!

# wait for WAITING_REVIEW, then find the PENDING review and decide it
REV_ID=""
for _ in $(seq 1 150); do
  stage="$($VQ workflow show "$TASK_ID" 2>/dev/null | sed -n 's/^stage \([A-Z_]*\).*/\1/p')"
  if [ "$stage" = "WAITING_REVIEW" ]; then
    REV_ID="$($VQ review query "$TASK_ID" 2>/dev/null | sed -n 's/.*"id":"\(rev-[^"]*\)".*/\1/p' | head -1)"
    [ -z "$REV_ID" ] && REV_ID="$(grep -o 'review [a-z0-9-]*' "$DATA/wf.out" | awk '{print $2}' | head -1)"
    [ -n "$REV_ID" ] && break
  fi
  sleep 0.1
done
[ -n "$REV_ID" ] || { echo "FAIL: workflow never reached WAITING_REVIEW with a review id"; cat "$DATA/wf.out"; exit 1; }

$VQ review decide "$REV_ID" -approved -reviewer alice -acceptance AC1=pass | grep -q APPROVED \
  || { echo "FAIL: review decide"; exit 1; }

wait "$WF_PID" || { echo "FAIL: workflow run exited non-zero"; cat "$DATA/wf.out"; exit 1; }
grep -q 'outcome DONE' "$DATA/wf.out" || { echo "FAIL: workflow outcome not DONE: $(cat "$DATA/wf.out")"; exit 1; }

# Task is DONE, and a SessionRecord exists.
$VD task show "$TASK_ID" | grep -q 'status DONE' || { echo "FAIL: task not DONE"; exit 1; }
$VQ session query "$TASK_ID" 2>/dev/null | grep -q 'sess-' || { echo "FAIL: no session record"; exit 1; }

restart_kernel
for _ in $(seq 1 50); do $VD task show "$TASK_ID" 2>/dev/null | grep -q 'status DONE' && break; sleep 0.1; done
$VD task show "$TASK_ID" 2>/dev/null | grep -q 'status DONE' || { echo "FAIL: DONE lost on restart"; exit 1; }

echo "M1.6 WORKFLOW SMOKE: OK"
```

- [ ] **Step 1–2: write + run** — `bash scripts/smoke.sh` → all fragments `OK`, ending `M1.6 WORKFLOW SMOKE: OK` then `M1 SMOKE: PASSED`.

  Adjust selectors (`review query` output shape, `workflow show` stage line) to what the CLI actually prints — the plan's `sed` patterns are a starting point; use `-json` where a stable field is easier to grep.

- [ ] **Step 3: run ×5** — no flake. If "DONE lost on restart" appears, it is the same class M1.4 fixed (`kill_kernel_tree`); the bounded retry above should cover it — bump `seq 1 50` → `seq 1 100` if a slow container needs it.

- [ ] **Step 4: commit** — `feat(m1.6): end-to-end workflow smoke (background run + human review + DONE + restart)`

---

## Task 10: acceptance gate + PR

**Files:** none — **do not touch `docs/M1-DESIGN.md`.**

- [ ] **Step 1: full build** — three module paths → exit 0.
- [ ] **Step 2: all Go tests** — `go test ./plugins/... ./plugins/_template ./cli/... && (cd kernel && go test ./...)` → all `ok` (includes the new `TestWorkQuery_*` in work-registry).
- [ ] **Step 3: kernel regression** — `cd kernel && ./scripts/build.sh >/dev/null && python3 tests/integration/m05_qualification.py 2>&1 | tail -2` → `PASSED`.
- [ ] **Step 4: architecture checks** — `bash scripts/check-arch.sh` → `CONTRACT CHECK: PASSED (32 contracts, ...)` (29 pre-M1.6 + `workflow.engineering.run/get` + `work.query` — trust the command's own count and adjust this literal if it differs), `COMPOSITION FITNESS: PASSED (10 manifests)` (with the `info:` line for the workflow), `ARCHITECTURE FITNESS: PASSED`, `ARCH CHECKS OK`. Also `python3 architecture-tests/check_composition_test.py` → all `ok`.
- [ ] **Step 5: smoke ×5** — every run ends `M1.6 WORKFLOW SMOKE: OK` + `M1 SMOKE: PASSED`, no `FAIL`; the earlier fragments (`WORKSPACE`/`AGENT`/`ARTIFACT+TOOL`/`REVIEW+SESSION`) all still `OK`.
- [ ] **Step 6: G1 + design purity** — `git diff --name-only 02e1014 HEAD -- kernel/internal kernel/cmd kernel/sdk` and `-- docs/M1-DESIGN.md` both empty.
- [ ] **Step 7: open the PR** — `chatgpt/m1-6-engineering-workflow` → `main`, title **M1.6 — Engineering Workflow (composition)**, body: the 11 tasks (1, 2, 2A, 3–10), verbatim acceptance output (Steps 3–6), deviations. No docs commit.

---

## Self-Review

**Spec coverage (`docs/M1-DESIGN.md` §13 M1.6 = "engineering-workflow（无状态编排 + WAITING_REVIEW 轮询 + DONE gate）+ FIX-4 composition fitness function"):**
- Stateless synchronous orchestration of the full §3 chain → Tasks 4, 6 (`runPipeline`, handler passes `rc.Context()`, watches no socket).
- WAITING_REVIEW = polling `review.get` (§7) → Task 4 step 10.
- DONE gate conjunction (§4.3) evaluated before `work.transition(DONE)`; gate fail ⇒ no DONE, Task stays IN_REVIEW, session still sealed → Tasks 3, 4.
- FIX-4 / A1: `check_composition.py` now binds a real composition plugin — exempt from fan-in ceiling, hard-required to own no stateful export → Task 1.
- Policy per §4.2: `local-cli` has no direct `work.transition@1`; only the `workflow.engineering.run@1` delegation scope reaches it; `org.vibe.workflow.engineering` grant carries the child capabilities; no `work.complete`, no crypto token → Task 7. The "external direct `work.transition(DONE)` denied" case (§4.4) is asserted in the smoke (Task 9) as an M1.7 preview.
- `workflow.engineering.get` = journal projection by `work_context_id`, derived (§7) → Task 5.
- canonical events (§9) appended at each step → Task 4.
- ADR-002 (Human Console interaction model): the console's context switcher needs to enumerate WorkContexts → Task 2A adds the additive read-only `work.query@1` + `vibe work list`. Everything else ADR-002 needs is already in the frozen design (WorkContext as the spine keyed by `work_context_id`; `vibe task show` as a read projection; changeset = diff artifact `summary.files[]`; the worktree is a real local directory the IDE lens drives directly). No contract, authority, or state-machine change.
- Deferred correctly: the M1.7 adversarial battery (stale review / wrong-diff / failed-test-cannot-DONE as *dedicated* qualifications — the smoke only previews the direct-denial); the real CLI provider (M1.8); real `mvn` commands (M1.9 — smoke uses `sh -c true`); automatic rework/reconciler and `service_authority` (M2, §7).

**Do-not-touch:** `docs/M1-DESIGN.md` is not edited or committed.

**Type consistency:** `EvidenceOutcome` / `ReviewState` / `doneGate` — Task 3, used in Task 4. `caps` / `EvItem` / `RunRequest` / `RunResult` / `runPipeline` — Task 4, used in Task 6. `JournalRecord` / `project` — Task 5, used in Task 6 (with the `payload.work_context_id` filter refinement noted). `runHandler` / `getHandler` — Task 6. Manifest `composition: true` + stateless exports + 14 `consumes` — Task 6, matches the `check_composition.py` rule from Task 1 and the policy grant from Task 7. Token strings `m1-local-cli-token` (unchanged) / `m1-dev-token` (new) consistent between policy, `scripts/smoke.sh`, and the fragment smokes.
