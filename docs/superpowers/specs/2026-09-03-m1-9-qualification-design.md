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

0. **No kernel / plugin / contract change.** `git diff <base> HEAD` touches nothing under `kernel/`, `plugins/`, or `contracts/`, and does not modify `scripts/check-arch.sh`, `scripts/smoke.sh`, `scripts/smoke-*.sh`, or `scripts/qualify-done-integrity.sh`. Only new `scripts/qualify-m1.sh` + `scripts/lib/console-projection.sh` + docs.
1. **Real provider is mandatory.** The qualifying run uses `provider=codex`. A run with `provider=mock`, or with `-mock-write-file`, or with `sh -c true` as build/test, is **not** a qualifying run and must not produce a PASS.
2. **No SKIP-as-PASS.** `qualify-m1.sh` requires `VIBE_REAL_PROVIDER=codex` and a working codex/mvn/java. If any precondition is missing it prints `SKIP: …` and exits 0 **without** printing `M1 ENGINEERING VERTICAL SLICE: PASSED`, without writing `docs/M1-RESULT.md`, and without creating the tag.
3. **Projection reads only the public query surface.** The Console-projection assertion library calls **only** `work.get`, `workspace.get`, `agent.run.query`, `artifact.query`, `tool.run.query`, `review.query`, `session.query`. It never reads a plugin's private JSONL log or state directory, and never uses `git diff` (or any out-of-band source) to fill a field the queries did not return. `blob.get` (foundation, public) may resolve a ref that one of those queries returned.
4. **致残 mutates data, not tests.** The projection 致残 check nulls `Artifact.summary.files[]` **in the projection input data** and re-invokes the assertion library; it does not delete an assertion or re-run the real workflow.
5. **Client disconnect must not cancel the workflow.** The qualifying run kills the `vibe workflow run` client process while the workflow is parked at `WAITING_REVIEW`, then proves — via an independent `workflow.get` poll from a different client — that the workflow still reaches `DONE`/`SEALED` after the review is decided.
6. **codex must not commit.** The task prompt explicitly forbids `git commit`. `artifact.collect_diff` and `RecoveryCheckpoint.tracked_patch_ref` are both built from the **uncommitted working-tree diff relative to HEAD** (`git diff HEAD`); a commit would empty them.
7. **"kill agent runtime" = restart the agent-harness plugin runtime.** By the time `agent.run` returns, the codex subprocess has already exited (`real_provider.go` waits for it before returning). M1.9 does **not** claim to verify recovery of an in-flight pre-DONE codex process. G5 kills and restarts the kernel and all plugin processes (including agent-harness) *after* DONE+seal and re-queries.
8. **The checkpoint is verified for what it actually carries.** With zero plugin change, `engineering-workflow` calls `session.seal` with only `{work_context_id, agent_run_id, workspace_path}` (`pipeline.go:25`, `handlers.go:264`) — so the produced `RecoveryCheckpoint` reliably carries `work_context_id`, `agent_run_id`, `base_commit`/`head_commit`/`branch`/`dirty`/`untracked_manifest`, `tracked_patch_ref`, `canonical_event_selection`, and **not** `task_id` / `provider` / `diff_artifact_id` (those `sealRequest` fields exist and `sealOnce` would populate them, but the caller never sends them). M1.9 asserts consistency of the *reconstructed WorkContext view* — provider from the `AgentRun` projection, diff from the `Review`/`Artifact` projections, task from `work.get` — not from checkpoint fields the current flow leaves empty. Enriching the seal call is a trivial later wiring change (session plugin already accepts the fields); it is **out of M1.9 scope** and listed in §9.
9. **Shell hygiene (the M1.5–M1.7 lessons).** `set -euo pipefail`; every assertion captures command output into a variable and matches with `case` — never `cmd | grep -q` / `grep -o | head` (SIGPIPE trips `pipefail`). Env guards use `${VAR:-}`. Any command whose non-zero exit is expected (`kill`/`wait` returning 143, `vibe workflow run` exiting non-zero on a non-DONE outcome, `git apply --check` in 致残) is run as `… || true` / captured `$?` and asserted explicitly.

**Inheritable:** `scripts/lib/kernel-harness.sh` lifecycle (`build_bins`, `restart_kernel`, tree-kill, EXIT trap); `scripts/smoke-workflow.sh` background-start / `WAITING_REVIEW` poll / second-terminal decide skeleton; M1.8's real-provider precondition checks (`scripts/verify-real-provider.sh`).
**Not inheritable:** `smoke-workflow.sh`'s auto-approve, `-mock-write-file`, `sh -c true`, dynamic `-f <ws>` build commands, reading private logs, using `vibe task show` in place of the full projection.

## 3. §10 qualification task (replaces the current M1-DESIGN §10 task text)

The current §10 task ("对非法输入（null / 溢出）增加指定行为") is **not decidable**: `Calculator.add(int, int)` cannot receive `null`, and "溢出增加指定行为" is undefined. Replace with:

```text
goal:  用 Math.addExact 改造 Calculator.add，整数溢出时抛 ArithmeticException，
       并补充正溢出、负溢出的测试。
scope: 只允许改 src/main/java/com/example/calc/Calculator.java
       和 src/test/java/com/example/calc/CalculatorTest.java。
acceptance_criteria:
  AC1: mvn -q test 成功
  AC2: 新增测试覆盖 Integer.MAX_VALUE 溢出 和 Integer.MIN_VALUE 溢出
  AC3: git diff + git ls-files --others 涉及的文件仅
       src/main/java/com/example/calc/Calculator.java
       与 src/test/java/com/example/calc/CalculatorTest.java
```

All path assertions use these **repo-relative** paths — `artifact.collect_diff` and
`git` report `src/main/java/com/example/calc/Calculator.java`, not a bare filename.

Prompt to codex (verbatim intent): *"改 `Calculator.add` 用 `Math.addExact`，溢出时让它自然抛 `ArithmeticException`；在 `CalculatorTest` 补两个测试，一个触发 `Integer.MAX_VALUE + 1` 正溢出、一个触发 `Integer.MIN_VALUE - 1` 负溢出，断言抛 `ArithmeticException`。**不要执行 `git commit`，把改动留在工作树里。** 只改这两个文件。"*

The two existing tests (`addsTwoPositiveNumbers` → `addExact(2,3)==5`, `addsNegativeNumbers` → `addExact(-1,-1)==-2`) still pass under `Math.addExact`, so no test conflict. The fixture already contains `Calculator.add` + `CalculatorTest`; **no fixture change is needed** for M1.9.

## 4. Components

### 4.1 `scripts/qualify-m1.sh` (new) — the acceptance script

```
preconditions (each → SKIP exit 0, no PASS/RESULT/tag if unmet):
  [ "${VIBE_REAL_PROVIDER:-}" = codex ]
  command -v codex   ; codex --version    (record)
  command -v mvn     ; mvn -v             (record Maven + Java version)
  command -v java

full-suite gate (must pass before the real run is even attempted; §8):
  bash scripts/build.sh ; go test ./... (3 modules) ; kernel M0.5 ;
  bash scripts/check-arch.sh ; bash scripts/smoke.sh (×5) ;
  bash scripts/qualify-done-integrity.sh (×3)   ← this run IS G4; not repeated later

setup:
  export VIBE_AGENT_PROVIDERS=codex        # BEFORE restart_kernel, so discovery registers codex
  source scripts/lib/kernel-harness.sh     # DATA, SOCK, tokens, restart_kernel, EXIT trap
  build_bins
  restart_kernel
  SRC = a throwaway `git clone` of fixtures/sample-java-project into $DATA, then one
        commit on branch 'main' (so base_commit / HEAD are commits *in SRC*, and the
        worktree the workflow allocates from SRC starts clean)

run  (see §5 for the exact WAITING_REVIEW / disconnect protocol):
  as local-cli:  vibe workflow run <task> -provider codex
                   -build "mvn -q -DskipTests compile"   -test "mvn -q test"
                   -timeout 30m
  → kill the client at WAITING_REVIEW, verify the workflow survives
  as m1-dev:     resolve review_id (JSON parse) ; inspect the diff (resolve diff_artifact_id
                 → artifact.get / blob.get, or read the worktree) ; review decide … --approved
  independent poll: workflow.engineering.get until stage DONE, then SEALED

gates:   run the G1..G6 assertions of §7, each capturing output into a variable
         and matching with `case` (never `cmd | grep -q` — pipefail/SIGPIPE flake)

on all green (in this order):
  write docs/M1-RESULT.md (§7 evidence, filled with the real ids/hashes from this run)
  re-run the G1 final whitelist check *including* the just-written M1-RESULT.md
  print "M1 ENGINEERING VERTICAL SLICE: PASSED"
  print the exact `git tag -a m1.9-qualification …` command for the reviewer to run
  (the script does NOT create the tag itself — tagging stays a manual reviewer step,
   same as every prior milestone)
```

Structural notes:
- **build/test commands are fixed and relative.** `tool-runner` sets the child's working directory to the allocated workspace (`plugins/tool-runner/runner.go:24`), and `workspace.allocate` happens *inside* the workflow — the script never knows the worktree path when it starts `vibe workflow run`. So the commands are `mvn -q -DskipTests compile` / `mvn -q test` with no `-f`; `pom.xml` resolves from the worktree cwd.
- `.query` capabilities are not friendly `vibe` subcommands; the script and the library call them via `.bin/vibe-raw -cap <name> -kind query -service <svc> -authority <auth> -payload …`, exactly as `scripts/verify-real-provider.sh` already does for `agent.run.query` / `blob.get`. Exact `-service`/`-authority` values per `contracts/catalog.json` + existing smoke scripts; the implementation plan pins them to an explicit list.
- codex's first real run may take 1–3 min; `-timeout 30m` and the poll bounds in §5 accommodate it.

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

1. **IDE-lens fields present**: `workspace.path`, `workspace.base_commit`; `Artifact{kind=diff}.summary.files[]` **non-empty**; every path in `summary.files[]` equals one of the two repo-relative scoped paths (`src/main/java/com/example/calc/Calculator.java`, `src/test/java/com/example/calc/CalculatorTest.java`) — no out-of-scope file.
2. **Agent-lens fields present**: exactly one `AgentRun` for this wc with `{id, provider, status, raw_session_ref}` all populated; `provider == "codex"` (**not** `mock`); `status == "COMPLETED"`.
3. **Truth chain wired**: the `Review` (from `review.query`) has `status == "APPROVED"` and `diff_artifact_id ==` the `Artifact{kind=diff}.id`; the two `EvidenceRef`s (from `work.get`'s WorkContext) reference the `ToolRun` ids from `tool.run.query` (one build, one test), and both ToolRun outcomes are success.
4. **Session record present and internally consistent**: `session.query` returns exactly one `SessionRecord`; its `RecoveryCheckpoint` has `work_context_id ==` the wc, `agent_run_id ==` the `AgentRun.id` from assertion 2, and non-empty `base_commit` / `head_commit` / `branch` / `tracked_patch_ref` / `canonical_event_selection`. (The checkpoint does **not** carry `task_id` / `provider` / `diff_artifact_id` under the current zero-plugin-change flow — invariant 8 — so those are asserted from the other projections, not here.)
5. **View is reconstructable and consistent across projections**: taken together, the 7 projections yield one coherent WorkContext view — the `provider` (from AgentRun) is `codex`, the diff artifact (from Review ↔ Artifact) is the same object referenced by the workflow, the task (from `work.get`) is the one under test. This is the ADR-002 "既有读投影足以支撑两个镜头" assertion; it is satisfied by the projection set *jointly*, which is how the Console consumes them.
6. **No private reads / no backfill**: the library contains no path into a plugin data dir and no `git` call. (Enforced by review of the library source, and by the `file:` mode working with only the 7 JSON blobs present.)

### 4.3 `docs/M1-DESIGN.md` edits (part of this milestone)

- **§10**: replace the task text with §3 above; replace `vibe review show <task-id>` (wrong — the CLI's `review show` takes a `<review-id>`, `cli/vibe/main.go:839`) with the two-step *"take `review_id` from `vibe workflow show <task-id> -json`, then `vibe review show <review-id>`; inspect the diff via `diff_artifact_id` → `artifact.get`/`blob.get` or the worktree"*; note codex-must-not-commit; note the checkpoint carries wc/agent_run/patch-ref but not task/provider/diff-artifact under the M1 flow (invariant 8).
- **§2**: expand each of G1–G6 with the concrete assertion from §7 (currently one-line criteria).
- **§13**: mark `M1.9 … — done` with the result summary; state "M1 PASSED" is recorded in `docs/M1-RESULT.md`.

### 4.4 `docs/M1-RESULT.md` (new, produced by the run)

Written **only** when `qualify-m1.sh` reaches all-green. Contains: the run's real ids (task, wc, agent_run, diff artifact, both tool runs, review, session), the codex/Maven/Java versions, the six G-gate evidence blocks with actual values, and the one-sentence M1 thesis verdict (§1 of M1-DESIGN). This file is the durable proof that M1 passed.

## 5. WAITING_REVIEW / client-disconnect protocol

```
1. start the client in its own process so the pid IS the vibe process:
     ( exec .bin/vibe -socket "$SOCK" -identity local-cli -token "$TOKEN" \
         workflow run "$TASK" -provider codex \
         -build "mvn -q -DskipTests compile" -test "mvn -q test" -timeout 30m \
     ) >"$DATA/wf.out" 2>&1 &
     CLIENT_PID=$!
2. poll `vibe workflow show <task>` until "stage WAITING_REVIEW".
   Bound the whole wait by the run deadline, not a fixed 2 min: up to ~1800 × 1s
   (30 min), since the codex step alone can take 1–3 min and mvn adds more.
   If CLIENT_PID exits before WAITING_REVIEW is seen → FAIL, dump wf.out.
3. REVIEW_ID: `vibe workflow show <task> -json` piped to a python3 one-liner that
   json-loads and prints `review_id` (NOT grep — SIGPIPE/pipefail, the M1.5/M1.6 flake).
4. client disconnect:
     kill -TERM "$CLIENT_PID"
     set +e; wait "$CLIENT_PID"; rc=$?; set -e
     # SIGTERM to a process blocked on the socket read → 143 (or 130 for SIGINT).
     # rc==0 would mean the client finished on its own → we did NOT test a disconnect.
     case "$rc" in 143|130) : ;; 0) echo "FAIL: client exited 0 — no disconnect tested"; exit 1 ;;
                   *) echo "FAIL: unexpected client exit $rc"; exit 1 ;; esac
5. as m1-dev (a different identity/token — the "second terminal"):
     RID from step 3
     inspect the diff: `vibe review show "$RID"` gives review metadata + diff_artifact_id;
       resolve it (`artifact.get` → blob, or read the allocated worktree) and eyeball it
     `vibe review decide "$RID" --approved --acceptance AC1=pass AC2=pass AC3=pass`
6. independent completion check — NOT the killed client's exit code:
     poll `vibe workflow show <task>` (query path = workflow.engineering.get) until
     stage is DONE, then until `session.query` returns a SessionRecord (SEALED).
     bounded ~120s.
7. restart_kernel      ← kills kernel + all plugin children, restarts, waits query-ready
8. run §7 G1..G6 + §4.2 projection assertion + §6 致残
```

If step 6 never reaches DONE after a valid decide, the workflow **did** get cancelled by the disconnect → invariant 5 fails → M1.9 FAIL.

## 6. 致残 sweep

| # | mutation | expected |
|---|---|---|
| D1 | in the projection `file:` snapshot, set `Artifact{kind=diff}.summary.files` to `[]`, call `assert_console_projection file:<dir> <task_id>` | FAIL at assertion 1 ("summary.files empty") |
| D2 | in the snapshot, change the `AgentRun.provider` (`agent.run.query` result) to `"mock"`, call the helper | FAIL at assertion 2 ("provider is not codex") |
| D3 | in the snapshot, point `Review.diff_artifact_id` at a non-existent id | FAIL at assertion 3 ("review does not reference the diff artifact") |
| D4 | take the blob behind `RecoveryCheckpoint.tracked_patch_ref`, corrupt one hunk header, `git apply --check` it against a clean checkout of **SRC** at `base_commit` | `git apply --check` exits non-zero (proves G6 actually applies the patch, not just reads the ref) |
| D5 | G4 is covered by re-running `scripts/qualify-done-integrity.sh` — its own M-S1..M-S4 sweep stands | (unchanged) |

Each mutation operates on a **copy** of the fetched projection JSON, never on live state. After the sweep the snapshot dir is discarded; `qualify-m1.sh` itself only ever reads live state, so a normal run leaves nothing mutated. `git apply --check` in D4 is run as `… || rc=$?` and the non-zero asserted explicitly (invariant 9).

## 7. G1–G6 evidence matrix

| Gate | Assertion (all captured into variables, matched with `case`) |
|---|---|
| **G1** Kernel Purity | `python3 kernel/architecture-tests/check_boundaries.py` exit 0; `git diff --name-only <base> HEAD -- kernel` empty; `git diff --name-only <base> HEAD` equals the exact file list the implementation plan enumerates (no wildcard, no `…`) — known members: `scripts/qualify-m1.sh`, `scripts/lib/console-projection.sh`, `docs/M1-DESIGN.md`, this spec, the plan file, `docs/M1-RESULT.md`. Re-run **after** M1-RESULT.md is written. |
| **G2** Real Execution | `AgentRun.provider == "codex"`; the allocated worktree has a non-empty `git diff HEAD` **and** `git ls-files --others --exclude-standard` lists no file outside the scoped set (untracked files bypass `git diff`); the changed+untracked set equals the two scoped paths; `Artifact{kind=diff}.summary.files[]` matches that set. codex stdout is **not** consulted. |
| **G3** Truth Chain | Task / WorkContext / Workspace / AgentRun / Artifact(diff) / ToolRun(build) / ToolRun(test) / EvidenceRef×2 / Review / SessionRecord — every object's `work_context_id` equals the one WorkContext id; every cross-reference id (`diff_artifact_id`, evidence `source_id`s, `agent_run_id`) resolves to a sibling object; build cmd == `mvn -q -DskipTests compile` and outcome success; test cmd == `mvn -q test` and outcome success. |
| **G4** DONE Integrity | the full-suite gate's `bash scripts/qualify-done-integrity.sh` ×3 (each ending `DONE-INTEGRITY QUALIFICATION: OK`) **is** G4 — run once, up front. The happy-path DONE of this milestone does **not** substitute for it. |
| **G5** Persistence | After DONE + seal, `restart_kernel` (kernel + all plugin processes killed and restarted). Then re-query all of G3's objects and resolve every raw ref (`raw_session_ref`, `archive_ref`, `tracked_patch_ref`) via `blob.get` → all still present, byte lengths > 0. |
| **G6** Recovery | `RecoveryCheckpoint.tracked_patch_ref` resolves via `blob.get`; a fresh `git clone` of **SRC** (the run's own clone, not `fixtures/sample-java-project` — `base_commit` is a commit in SRC) + `git checkout <base_commit>` + `git apply` of that patch succeeds and reproduces the two scoped files; `RecoveryCheckpoint.{work_context_id, agent_run_id, base_commit}` match the projections. Field-non-empty alone is **not** sufficient — the patch must actually apply. |

## 8. Acceptance (what "M1.9 done" means)

"M1 PASSED" is not just this one scenario — it is the whole M1 suite green **plus** the real end-to-end run.

1. **Full regression green first** (the script gates on this before attempting the real run): `bash scripts/build.sh`; `go test ./...` across the 3 modules; kernel `python3 tests/integration/m05_qualification.py` → PASSED; `bash scripts/check-arch.sh` → `31 contracts` / `10 manifests` / `ARCH CHECKS OK`; `bash scripts/smoke.sh` ×5 → `M1 SMOKE: PASSED`, no orphans; `bash scripts/qualify-done-integrity.sh` ×3 → `DONE-INTEGRITY QUALIFICATION: OK`.
2. `VIBE_REAL_PROVIDER=codex bash scripts/qualify-m1.sh` on the dev machine prints `M1 ENGINEERING VERTICAL SLICE: PASSED`, with G1–G6 evidence in the output.
3. The 致残 sweep D1–D4 each reproduce the expected failure, then restore green; G4/D5's sub-sweep passes.
4. `docs/M1-RESULT.md` is generated (before the PASSED line is printed) and committed with the real run's ids/hashes.
5. `docs/M1-DESIGN.md` §2 / §10 / §13 reconciled.
6. Reviewer independently re-runs `qualify-m1.sh` once and re-does the 致残 sweep (校验者≠生产者).
7. Tag `m1.9-qualification` created on the commit that carries all of the above (manual `git tag`, per the script's printed command).
8. `git diff <base> HEAD -- kernel` empty; no plugin/contract change; the changed-file set matches the plan's enumerated list exactly.

## 9. NON-GOALS

- `work.query@1` / context enumeration — Console v1 (subsystem B), not M1.9.
- Any Console/TUI/GUI code — subsystem B.
- **Enriching the `session.seal` call** so `RecoveryCheckpoint` carries `task_id` / `provider` / `diff_artifact_id` — the session plugin already accepts these; wiring `engineering-workflow` to send them is a ~3-line change but it makes M1.9 no longer zero-plugin-change. Deferred (M2, or a standalone follow-up). M1.9 asserts view consistency from the other projections instead (invariant 8).
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
- **Field ownership:** `provider` is proven by the `AgentRun` projection; the diff by the `Artifact`/`Review` projections; recovery facts by the `Session` projection. The `RecoveryCheckpoint` is not treated as the source of truth for task/provider/artifact identity under the M1 flow (invariant 8).
- **RPC boundary:** no new contract. If a future revision insists the checkpoint carry task/provider/diff-artifact, that is a plugin change and re-scopes M1.9 — call it out, don't slip it in.
- **Admission:** freezable once (a) §3 task is decidable with real paths, (b) invariant 7 runtime semantics, (c) invariant 8 + §4.2 assertion 4 reconcile the checkpoint claims with the real seal payload, (d) §4.1/§5 build commands are fixed & relative, (e) §6 mutates snapshot data not tests, (f) §7 G1–G6 matrix — all now in the written spec. Exact `-service`/`-authority` values and the final file whitelist are pinned by the implementation plan, not this spec.
