# M1.9 — M1 Qualification (real provider end-to-end + G1–G6) — Design

**Status:** for review (2026-09-03)
**Spec source:** `docs/M1-DESIGN.md` §2 (G1–G6), §10 (qualification scenario), §13 (milestone M1.9), ADR-002 (Console read-projection sufficiency acceptance), ADR-003 (frontend deferred — unaffected here).
**Milestone position:** after M1.8 (`m1.8-real-provider-adapter`, main `c640957`), last milestone of M1. On success → **"M1 ENGINEERING VERTICAL SLICE: PASSED"**, then M2.
**Execution:** dev-machine only, run once by the reviewer. Not dispatched, not in CI. codex-cli 0.152.1 + Maven 3.9.12 + Java 8 confirmed present.

---

## 1. Goal

Prove the whole locked engineering workflow runs on **a real coding agent (codex) making a real change verified by a real build/test**, and that every M1 gate (G1–G6) holds with **explicit, cross-checked evidence** — not a green happy-path. The load-bearing new check is ADR-002's assertion that the existing read-projections are sufficient to reconstruct a WorkContext view for the future Console, made **falsifiable** here so it cannot surface as expensive rework during the UI phase.

M1.9 adds **no kernel change, no plugin change, no contract change**. It is a qualification harness plus documentation reconciliation.

## 2. Design invariants (must hold at merge)

0. **No kernel / plugin / contract change.** `git diff <base> HEAD -- kernel` empty; no file under `plugins/*/` (except none) changes; `contracts/` unchanged; `scripts/check-arch.sh`, `scripts/smoke.sh`, `scripts/qualify-done-integrity.sh` unchanged.
1. **Real provider is mandatory.** The qualifying run uses `provider=codex`. A run with `provider=mock`, or with `-mock-write-file`, or with `sh -c true` as build/test, is **not** a qualifying run and must not produce a PASS.
2. **No SKIP-as-PASS.** `qualify-m1.sh` requires `VIBE_REAL_PROVIDER=codex` and a working codex/mvn/java. If any precondition is missing it prints `SKIP: …` and exits 0 **without** printing `M1 ENGINEERING VERTICAL SLICE: PASSED`, without writing `docs/M1-RESULT.md`, and without creating the tag.
3. **Projection reads only the public query surface.** The Console-projection assertion library calls **only** `work.get`, `workspace.get`, `agent.run.query`, `artifact.query`, `tool.run.query`, `review.query`, `session.query`. It never reads a plugin's private JSONL log or state directory, and never uses `git diff` (or any out-of-band source) to fill a field the queries did not return. `blob.get` (foundation, public) may resolve a ref that one of those queries returned.
4. **致残 mutates data, not tests.** The projection 致残 check nulls `Artifact.summary.files[]` **in the projection input data** and re-invokes the assertion library; it does not delete an assertion or re-run the real workflow.
5. **Client disconnect must not cancel the workflow.** The qualifying run kills the `vibe workflow run` client process while the workflow is parked at `WAITING_REVIEW`, then proves — via an independent `workflow.get` poll from a different client — that the workflow still reaches `DONE`/`SEALED` after the review is decided.
6. **codex must not commit.** The task prompt explicitly forbids `git commit`. `artifact.collect_diff` and `RecoveryCheckpoint.tracked_patch_ref` are both built from the **uncommitted working-tree diff relative to HEAD** (`git diff HEAD`); a commit would empty them.
7. **"kill agent runtime" = restart the agent-harness plugin runtime.** By the time `agent.run` returns, the codex subprocess has already exited (`real_provider.go` waits for it before returning). M1.9 does **not** claim to verify recovery of an in-flight pre-DONE codex process. G5 kills and restarts the kernel and all plugin processes (including agent-harness) *after* DONE+seal and re-queries.

**Inheritable:** `scripts/lib/kernel-harness.sh` lifecycle (`build_bins`, `restart_kernel`, tree-kill, EXIT trap); `scripts/smoke-workflow.sh` background-start / `WAITING_REVIEW` poll / second-terminal decide skeleton; M1.8's real-provider precondition checks (`scripts/verify-real-provider.sh`).
**Not inheritable:** `smoke-workflow.sh`'s auto-approve, `-mock-write-file`, `sh -c true`, reading private logs, using `vibe task show` in place of the full projection.

## 3. §10 qualification task (replaces the current M1-DESIGN §10 task text)

The current §10 task ("对非法输入（null / 溢出）增加指定行为") is **not decidable**: `Calculator.add(int, int)` cannot receive `null`, and "溢出增加指定行为" is undefined. Replace with:

```text
goal:  用 Math.addExact 改造 Calculator.add，整数溢出时抛 ArithmeticException，
       并补充正溢出、负溢出的测试。
scope: 只允许改 Calculator.java 和 CalculatorTest.java。
acceptance_criteria:
  AC1: mvn -q test 成功
  AC2: 新增测试覆盖 Integer.MAX_VALUE 溢出 和 Integer.MIN_VALUE 溢出
  AC3: git diff 涉及的文件仅 Calculator.java 与 CalculatorTest.java
```

Prompt to codex (verbatim intent): *"改 `Calculator.add` 用 `Math.addExact`，溢出时让它自然抛 `ArithmeticException`；在 `CalculatorTest` 补两个测试，一个触发 `Integer.MAX_VALUE + 1` 正溢出、一个触发 `Integer.MIN_VALUE - 1` 负溢出，断言抛 `ArithmeticException`。**不要执行 `git commit`，把改动留在工作树里。** 只改这两个文件。"*

The two existing tests (`addsTwoPositiveNumbers` → `addExact(2,3)==5`, `addsNegativeNumbers` → `addExact(-1,-1)==-2`) still pass under `Math.addExact`, so no test conflict. The fixture already contains `Calculator.add` + `CalculatorTest`; **no fixture change is needed** for M1.9.

## 4. Components

### 4.1 `scripts/qualify-m1.sh` (new) — the acceptance script

```
preconditions (each → SKIP exit 0, no PASS/RESULT/tag if unmet):
  [ "$VIBE_REAL_PROVIDER" = codex ]
  command -v codex   ; codex --version    (record)
  command -v mvn     ; mvn -v             (record Maven + Java version)
  command -v java

setup:
  source scripts/lib/kernel-harness.sh   # DATA, SOCK, tokens, restart_kernel, EXIT trap
  build_bins
  restart_kernel
  SRC = a throwaway clone of fixtures/sample-java-project committed to one 'main' commit
        (so base_commit / HEAD are well-defined and the worktree starts clean)

run  (see §5 for the exact WAITING_REVIEW / disconnect protocol):
  as local-cli:  vibe workflow run <task> -provider codex
                   -build "mvn -q -DskipTests -f <ws>/pom.xml compile"
                   -test  "mvn -q -f <ws>/pom.xml test"
                   -timeout 30m
  → kill the client at WAITING_REVIEW
  as m1-dev:     review show <review-id> ; review decide <review-id> --approved --acceptance AC1=pass AC2=pass AC3=pass
  independent poll: workflow.get until stage DONE and SEALED

gates:   run the G1..G6 assertions of §7, each capturing output into a variable
         and matching with `case` (never `cmd | grep -q` — pipefail/SIGPIPE flake)

on all green:
  print "M1 ENGINEERING VERTICAL SLICE: PASSED"
  write docs/M1-RESULT.md (§7 evidence, filled with the real ids/hashes from this run)
  print the exact `git tag -a m1.9-qualification …` command for the reviewer to run
  (the script does NOT create the tag itself — tagging stays a manual reviewer step,
   same as every prior milestone)
```

Structural notes:
- `.query` capabilities are not friendly `vibe` subcommands; the script and the library call them via `.bin/vibe-raw -cap <name> -kind query -service <svc> -authority <auth> -payload …`, exactly as `scripts/verify-real-provider.sh` already does for `agent.run.query` / `blob.get`. Exact `-service`/`-authority` values per `contracts/catalog.json` + existing smoke scripts; the implementation plan pins them.
- All `mvn` invocations pass `-f <ws>/pom.xml` so the build/test run against the allocated worktree, not the repo.
- codex's first real run may take 1–3 min; `-timeout 30m` and the internal poll bounds accommodate it.

### 4.2 `scripts/lib/console-projection.sh` (new) — reusable assertion library

A sourced library, not an executable. One entry point:

```
assert_console_projection <mode> <task_id>
    mode = "live"     → fetch the 7 projections from the running kernel
    mode = "file:DIR" → load the 7 projections from DIR/<capability>.json (for 致残 —
                        the caller fetches once, mutates a copy, points the library
                        at the copy)
```

Navigation mirrors the Console's: `work.get {task_id}` returns both the Task and its
WorkContext, so the `work_context_id` for the other six queries comes from that first
response — the library is handed only a `task_id`, never a pre-resolved wc. It
fetches / loads exactly these and nothing else:

| projection | capability | key |
|---|---|---|
| task view | `work.get` | `{task_id}` → yields `work_context_id` |
| workspace | `workspace.get` | `{work_context_id}` |
| agent runs | `agent.run.query` | `{work_context_id}` |
| artifacts | `artifact.query` | `{work_context_id}` |
| tool runs | `tool.run.query` | `{work_context_id}` |
| reviews | `review.query` | `{work_context_id}` |
| sessions | `session.query` | `{work_context_id}` |

Assertions (all must hold; any failure → non-zero + a specific message):

1. **IDE-lens fields present**: `workspace.path`, `workspace.base_commit`; `Artifact{kind=diff}.summary.files[]` **non-empty**; every path in `summary.files[]` is within the task scope (`Calculator.java`, `CalculatorTest.java`) — no out-of-scope file.
2. **Agent-lens fields present**: exactly one `AgentRun` for this wc with `{id, provider, status, raw_session_ref}` all populated; `provider == "codex"` (**not** `mock`); `status == "COMPLETED"`.
3. **Truth chain wired**: the `Review` (from `review.query`) has `status == "APPROVED"` and `diff_artifact_id ==` the `Artifact{kind=diff}.id`; the two `EvidenceRef`s on the review/work reference the `ToolRun` ids from `tool.run.query` (one build, one test), and both ToolRun outcomes are success.
4. **Session checkpoint consistent**: `session.query` returns one `SessionRecord`; its `RecoveryCheckpoint` fields `task_id / work_context_id / agent_run_id / provider / diff_artifact_id` equal the corresponding ids from the other six projections; `provider == "codex"`.
5. **No private reads / no backfill**: the library contains no path into a plugin data dir and no `git` call. (Enforced by review of the library source, and by the `file:` mode working with only the 7 JSON blobs present.)

### 4.3 `docs/M1-DESIGN.md` edits (part of this milestone)

- **§10**: replace the task text with §3 above; replace `vibe review show <task-id>` with the two-step *"take `review_id` from `vibe workflow show <task-id> -json`, then `vibe review show <review-id>`"*; note codex-must-not-commit.
- **§2**: expand each of G1–G6 with the concrete assertion from §7 (currently one-line criteria).
- **§13**: mark `M1.9 … — done` with the result summary; state "M1 PASSED" is recorded in `docs/M1-RESULT.md`.

### 4.4 `docs/M1-RESULT.md` (new, produced by the run)

Written **only** when `qualify-m1.sh` reaches all-green. Contains: the run's real ids (task, wc, agent_run, diff artifact, both tool runs, review, session), the codex/Maven/Java versions, the six G-gate evidence blocks with actual values, and the one-sentence M1 thesis verdict (§1 of M1-DESIGN). This file is the durable proof that M1 passed.

## 5. WAITING_REVIEW / client-disconnect protocol

```
1. start, backgrounded, as local-cli:
     vibe workflow run <task> -provider codex \
       -build "mvn -q -DskipTests -f <ws>/pom.xml compile" \
       -test  "mvn -q -f <ws>/pom.xml test" -timeout 30m  &   CLIENT_PID=$!
2. poll `vibe workflow show <task>` until "stage WAITING_REVIEW"
   (bounded: ~600 × 0.2s = 2 min after the codex run, which itself may take 1–3 min;
    also break if CLIENT_PID dies early — that is a failure, capture wf.out)
3. REVIEW_ID = from `vibe workflow show <task> -json`  (grep '"review_id":"…"')
4. kill -TERM "$CLIENT_PID"; wait "$CLIENT_PID"       ← client disconnect
5. as m1-dev (a different identity/token, i.e. a second "terminal"):
     vibe review show "$REVIEW_ID"            (inspect the diff)
     vibe review decide "$REVIEW_ID" --approved --acceptance AC1=pass AC2=pass AC3=pass
6. independent completion check — NOT the killed client's exit code:
     poll `vibe workflow show <task>` (query path = workflow.engineering.get) until
     stage is DONE, then until a SessionRecord exists (SEALED).  bounded ~60s.
7. restart_kernel      ← kills kernel + all plugin children, restarts, waits query-ready
8. run §7 G1..G6 + §4.2 projection assertion + §6 致残
```

If step 6 never reaches DONE after a valid decide, the workflow **did** get cancelled by the disconnect → invariant 5 fails → M1.9 FAIL.

## 6. 致残 sweep

| # | mutation | expected |
|---|---|---|
| D1 | in the projection `file:` snapshot, set `Artifact{kind=diff}.summary.files` to `[]`, call `assert_console_projection file:<dir> <wc>` | FAIL at assertion 1 ("summary.files empty") |
| D2 | in the snapshot, change `RecoveryCheckpoint.provider` to `"mock"` | FAIL at assertion 4 ("checkpoint provider mismatch / not codex") |
| D3 | take the blob behind `RecoveryCheckpoint.tracked_patch_ref`, corrupt one hunk header, `git apply --check` it against a clean `base_commit` checkout | `git apply` fails (proves G6 actually applies the patch, not just reads the ref) |
| D4 | G4 is covered by re-running `scripts/qualify-done-integrity.sh` — its own M-S1..M-S4 sweep stands | (unchanged) |

After the sweep the working tree and the projection snapshot dir are restored; `qualify-m1.sh` itself only ever reads live state, so a normal run leaves nothing mutated.

## 7. G1–G6 evidence matrix

| Gate | Assertion (all captured into variables, matched with `case`) |
|---|---|
| **G1** Kernel Purity | `python3 kernel/architecture-tests/check_boundaries.py` exit 0; `git diff --name-only <base> HEAD -- kernel` empty; `git diff --name-only <base> HEAD` ⊆ { `scripts/qualify-m1.sh`, `scripts/lib/console-projection.sh`, `docs/M1-DESIGN.md`, `docs/superpowers/specs/2026-09-03-m1-9-qualification-design.md`, `docs/superpowers/plans/…`, `docs/M1-RESULT.md` } |
| **G2** Real Execution | `AgentRun.provider == "codex"`; the allocated worktree has a non-empty `git diff HEAD` touching only the scoped files; `Artifact{kind=diff}.summary.files[]` matches that set. codex stdout is **not** consulted. |
| **G3** Truth Chain | Task / WorkContext / Workspace / AgentRun / Artifact(diff) / ToolRun(build) / ToolRun(test) / EvidenceRef×2 / Review / SessionRecord — every object's `work_context_id` equals the one WorkContext id; every cross-reference id (`diff_artifact_id`, evidence `source_id`s, `agent_run_id`) resolves to a sibling object; build cmd == the mvn compile argv and outcome success; test cmd == the mvn test argv and outcome success. |
| **G4** DONE Integrity | `bash scripts/qualify-done-integrity.sh` re-run ×3, each ends `DONE-INTEGRITY QUALIFICATION: OK`. The happy-path DONE of this milestone does **not** substitute for it. |
| **G5** Persistence | After DONE + seal, `restart_kernel` (kernel + all plugin processes killed and restarted). Then re-query all of G3's objects and resolve every raw ref (`raw_session_ref`, `archive_ref`, `tracked_patch_ref`) via `blob.get` → all still present, byte lengths > 0. |
| **G6** Recovery | `RecoveryCheckpoint.tracked_patch_ref` resolves via `blob.get`; a fresh `git clone` / `git checkout <base_commit>` of the fixture + `git apply` of that patch succeeds and reproduces the scoped file set; `RecoveryCheckpoint.{task_id,work_context_id,agent_run_id,provider}` match the projections. Field-non-empty alone is **not** sufficient. |

## 8. Acceptance (what "M1.9 done" means)

1. `VIBE_REAL_PROVIDER=codex bash scripts/qualify-m1.sh` on the dev machine prints `M1 ENGINEERING VERTICAL SLICE: PASSED`, with G1–G6 evidence in the output.
2. The 致残 sweep D1–D3 each reproduce the expected failure, then restore green; G4's sub-sweep passes.
3. `docs/M1-RESULT.md` is generated and committed with the real run's ids/hashes.
4. `docs/M1-DESIGN.md` §2 / §10 / §13 reconciled.
5. Reviewer independently re-runs `qualify-m1.sh` once and re-does the 致残 sweep (校验者≠生产者).
6. Tag `m1.9-qualification` created on the commit that carries all of the above.
7. `git diff <base> HEAD -- kernel` empty; no plugin/contract change.

## 9. NON-GOALS

- `work.query@1` / context enumeration — Console v1 (subsystem B), not M1.9.
- Any Console/TUI/GUI code — subsystem B.
- Auto-resume / reconciler / persistent orchestration state — M2 (§7 of M1-DESIGN).
- Verifying recovery of an in-flight pre-DONE codex subprocess (see invariant 7).
- Structured transcript rendering, interactive agent follow-up — M2 / UI phase.
- Fixing the M1.8 `probeVersion` discovery-test flake — separate follow-up, not this milestone.
- Wiring any of this into `check-arch.sh` / `smoke.sh` / CI.

## 10. Known limitations

- Single qualifying run against one task. It proves the chain works end-to-end once with a real agent, not that every prompt/agent/repo combination works.
- codex is non-deterministic: a run can fail because codex produced a bad patch or failing tests. That is a *real* negative result (the gate correctly refuses DONE), not a harness bug — re-run. The harness must surface *which* stage failed (agent / build / test / review / gate).
- Java 8 + JUnit 4 fixture; `Math.addExact` is Java 7+, fine.
- Maven downloads plugins on first run; the dev machine must have a warm `~/.m2` or network. Recorded as a precondition, not worked around.

## 11. Drift guardrails

- **Design invariant:** M1.9 changes no kernel/plugin/contract; the qualifying run is explicitly codex; the projection reads only the 7 public queries; a PASS cannot be produced by mock or by SKIP.
- **Field ownership:** `provider` is chosen by the caller; `status` is produced by the plugin; `Artifact.summary.files[]` comes from `artifact.query`; recovery facts come from `session.query`.
- **Admission:** freezable once §3 (task), invariant 7 (runtime semantics), §6 D1 (data 致残), and §7 (G1–G6 matrix) are in the written spec — which they now are.
