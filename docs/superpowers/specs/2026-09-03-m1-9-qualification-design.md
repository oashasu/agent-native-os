# M1.9 — M1 Qualification (real provider end-to-end + G1–G6) — Design

**Status:** rev10 for review (2026-09-04)
**Spec source:** `docs/M1-DESIGN.md` §2 (G1–G6), §10 (qualification scenario), §13 (milestone M1.9), ADR-002 (Console read-projection sufficiency acceptance), ADR-003 (frontend deferred — unaffected here).
**Milestone position:** after M1.8 (`m1.8-real-provider-adapter`; the post-merge documentation reconciliation is `c640957`), last milestone of M1. On success → **"M1 ENGINEERING VERTICAL SLICE: PASSED"**, then M2.
**Execution:** dev-machine only: one production run creates the durable result, followed by one independent verification rerun. Neither is dispatched or run in CI. The script records the installed codex-cli version at runtime (the M1.8 baseline was 0.152.1), plus the required Maven 3.9.12 / Java 8 versions and the installed Go version.

---

## 1. Goal

Prove the whole locked engineering workflow runs on **a real coding agent (codex) making a real change verified by a real build/test**, and that every M1 gate (G1–G6) holds with **explicit, cross-checked evidence** — not a green happy-path. The load-bearing new check is the reconstructable portion of ADR-002's public read-projection claim, made **falsifiable** here so it cannot surface as expensive rework during the UI phase. The result is deliberately conditional: because the current workspace query returns only allocated workspaces by `work_context_id`, the qualification resolves the stable `workspace_id` while the workflow is at `WAITING_REVIEW`, then re-queries the released workspace by that public id after restart. It proves the seven-projection object join with that public-id bridge; it does **not** close ADR-002's separate task-only navigation question for an already-released workspace.

M1.9 adds **no kernel change, no plugin change, no contract change**. It is a qualification harness plus documentation reconciliation.

**Decision boundary:** this spec is meaningful as the final M1 vertical-slice
qualification only if the retained public `workspace_id` is an accepted
qualification bridge. If the milestone is required to close the original
ADR-002 claim for cold-start, task-only navigation of a released workspace,
this is the wrong scope: the workspace API/read model must be changed first,
and the zero-plugin-change M1.9 result must remain **not approved**.

## 2. Design invariants (must hold at merge)

0. **No kernel / plugin / contract change.** `git diff <QUAL_BASE>` touches nothing under `kernel/`, `plugins/`, or `contracts`, and does not modify `scripts/check-arch.sh`, `scripts/smoke.sh`, `scripts/smoke-*.sh`, or `scripts/qualify-done-integrity.sh`. `QUAL_BASE` is captured after this spec and its implementation plan are committed and before implementation starts. The final implementation delta is exactly `scripts/qualify-m1.sh`, `scripts/lib/console-projection.sh`, `docs/M1-DESIGN.md`, and `docs/M1-RESULT.md`; the spec and plan are already at `QUAL_BASE` and are not part of that delta.
1. **Real provider is mandatory.** The M1.9 vertical-slice run uses `provider=codex`. A vertical-slice run with `provider=mock`, with `-mock-write-file`, or with `sh -c true` as build/test is **not** a qualifying run and must not produce a PASS. The inherited G4 `qualify-done-integrity.sh` suite is allowed to retain its deterministic mock fixtures; it is a separate adversarial gate, not the M1.9 real-provider run.
2. **No SKIP-as-PASS.** `qualify-m1.sh` requires `VIBE_REAL_PROVIDER=codex` and a working codex/mvn/java. If any precondition is missing it prints `SKIP: …` and exits 0 **without** printing `M1 ENGINEERING VERTICAL SLICE: PASSED`, without writing `docs/M1-RESULT.md`, and without creating the tag.
3. **Projection reads only the public query surface.** The Console-projection assertion library calls **only** `work.get`, `workspace.get`, `agent.run.query`, `artifact.query`, `tool.run.query`, `review.query`, `session.query`; it does not call `blob.get` itself. The acceptance harness may call the public `blob.get` only outside that library to resolve a reference already returned by a projection. Likewise, `workflow.engineering.get` is used only by the harness as a control-plane synchronization query for `WAITING_REVIEW`/`DONE`/`SEALED`; its stage/events never populate a projection field. Neither path reads a plugin's private JSONL log or state directory, or uses `git diff` to fill a field the seven projections did not return. The harness performs one public `workspace.get{work_context_id}` while the workspace is still `ALLOCATED`, records the returned `workspace_id`, and supplies that stable public selector to the post-restart live assertion; after release, `workspace.get{work_context_id}` is not claimed to work. The library entry point is therefore `assert_console_projection <mode> <task_id> [workspace_id]`; `live` requires the captured `workspace_id`, while `file:DIR` loads it from the snapshot. Its only non-projection input is the required `CONSOLE_PROJECTION_POLICY` comparison file; that file supplies expectations only and never fills an observed field.
4. **致残 mutates data, not tests.** The projection 致残 check nulls `Artifact.summary.files[]` **in the projection input data** and re-invokes the assertion library; it does not delete an assertion or re-run the real workflow.
5. **Client disconnect must not cancel the workflow.** The qualifying run kills the `vibe workflow run` client process while the workflow is parked at `WAITING_REVIEW`, then proves — via an independent `workflow.get` poll from a different client — that the workflow still reaches `DONE`/`SEALED` after the review is decided.
6. **codex must not commit.** The task prompt explicitly forbids `git commit`. `artifact.collect_diff` and `RecoveryCheckpoint.tracked_patch_ref` are both built from the **uncommitted working-tree diff relative to HEAD** (`git diff HEAD`); a commit would empty them.
7. **"kill agent runtime" = restart the agent-harness plugin runtime.** By the time `agent.run` returns, the codex subprocess has already exited (`real_provider.go` waits for it before returning). M1.9 does **not** claim to verify recovery of an in-flight pre-DONE codex process. G5 kills and restarts the kernel and all plugin processes (including agent-harness) *after* DONE+seal and re-queries.
8. **The checkpoint is verified for what it actually carries.** With zero plugin change, `engineering-workflow` calls `session.seal` with only `{work_context_id, agent_run_id, workspace_path}` (`pipeline.go:25`, `handlers.go:264`) — so the produced `RecoveryCheckpoint` reliably carries `work_context_id`, `agent_run_id`, `base_commit`/`head_commit`/`branch`/`dirty`/`untracked_manifest`, `tracked_patch_ref`, and `canonical_event_selection`, and leaves `task_id` / `provider` / `diff_artifact_id` / `harness_native_id` empty (those `sealRequest` fields exist and `sealOnce` would populate them, but the caller never sends them). The `session.seal` payload also includes neither `correlation_id` nor `event_ids`; because the child journal calls retain the external request correlation, the successful M1.6 workflow can legitimately produce an empty canonical-event selection. M1.9 therefore checks that the selection is internally consistent (`event_count == len(event_ids) == len(event_sha256s)`) and that the archive's `canonical_events` length agrees; it does **not** require a non-empty selection. M1.9 asserts consistency of the *reconstructed WorkContext view* — provider from the `AgentRun` projection, diff from the `Review`/`Artifact` projections, task from `work.get` — not from checkpoint fields the current flow leaves empty. For the successful uncommitted run, `checkpoint.base_commit == checkpoint.head_commit == workspace.base_commit`, `checkpoint.dirty == true`, `checkpoint.branch == workspace.branch`, and the ignored Maven output leaves `untracked_manifest == []`. Enriching the seal call or correcting workflow correlation/event-id wiring is a later change (the session plugin already accepts the fields); it is **out of M1.9 scope** and listed in §9.
9. **Shell hygiene (the M1.5–M1.7 lessons).** `set -euo pipefail`; every assertion captures command output into a variable and matches with `case` — never `cmd | grep -q` / `grep -o | head` (SIGPIPE trips `pipefail`). Env guards use `${VAR:-}`. Preconditions are evaluated in `if`/`case` contexts so `set -e` cannot bypass the required `SKIP` result. The script defines one `fail()` helper that prints the message, dumps the last 40 lines of `kernel.log` when available, and exits 1; full-suite failures and workflow-stage failures use it. Any command whose non-zero exit is expected (`kill`/`wait` returning 143, `vibe workflow run` exiting non-zero on a non-DONE outcome, `git apply --check` in 致残) is run as `… || true` / captured `$?` and asserted explicitly. An interrupt path terminates a still-live workflow client before the inherited harness `EXIT` trap removes `DATA`.
10. **The runtime source is a real clean scratch repository.** `fixtures/sample-java-project` is source material inside the main repository, not a standalone Git repository. The harness copies its contents into `$SRC`, initializes `$SRC` with branch `main`, adds an initial `.gitignore` containing `target/`, commits that baseline, and records `SRC_BASE`. The task uses `$SRC` as its repo; no Maven-generated `target/` file may enter the changed or untracked scope.
11. **Review snapshot identity follows the shipped M1.6 wiring.** `runPipeline` currently fills `EvItem.EvidenceRefID` with each ToolRun id because `caps.AttachEvidence` returns only an error; consequently `Review.evidence_snapshot[].evidence_ref_id` joins to the two ToolRuns, while the actual WorkContext EvidenceRef ids are checked independently through `EvidenceRef.source_id`. M1.9 must assert this exact, internally consistent shape and must not claim that the snapshot field equals `EvidenceRef.id`; correcting that field requires workflow/plugin wiring and is out of scope.

**Inheritable:** `scripts/lib/kernel-harness.sh` lifecycle (`build_bins`, `restart_kernel`, tree-kill, EXIT trap); `scripts/smoke-workflow.sh` background-start / `WAITING_REVIEW` poll / second-terminal decide skeleton; M1.8's real-provider precondition checks (`scripts/verify-real-provider.sh`).
**Not inheritable:** `smoke-workflow.sh`'s auto-approve, `-mock-write-file`, `sh -c true`, dynamic `-f <ws>` build commands, reading private logs, using `vibe task show` in place of the full projection.

## 3. §10 qualification task (replaces the current M1-DESIGN §10 task text)

The current §10 task ("对非法输入（null / 溢出）增加指定行为") is **not decidable**: `Calculator.add(int, int)` cannot receive `null`, and "溢出增加指定行为" is undefined. Replace with:

```text
title: M1.9 real overflow hardening
goal:  Use Math.addExact in Calculator.add and cover positive and negative integer overflow
scope: src/main/java/com/example/calc/Calculator.java,src/test/java/com/example/calc/CalculatorTest.java
acceptance_criteria:
  AC1: mvn -q test succeeds
  AC2: tests separately call Calculator.add(Integer.MAX_VALUE, 1) and Calculator.add(Integer.MIN_VALUE, -1) and assert ArithmeticException for both
  AC3: only the two scoped files are changed
```

The `title`, `goal`, `scope`, and acceptance-criterion texts above are canonical
Task values. The task-create command must use them byte-for-byte. The policy
below mirrors the title, goal, acceptance criteria, and declared files; its
file order is the source for the expected Task `scope`. The prompt is a
separate agent instruction and is not copied into the Task `goal`.

All path assertions use these **repo-relative** paths — `artifact.collect_diff` and
`git` report `src/main/java/com/example/calc/Calculator.java`, not a bare filename.

Prompt to codex (exact payload string):

```text
改 src/main/java/com/example/calc/Calculator.java 中的 Calculator.add，用 Math.addExact 让整数溢出自然抛 ArithmeticException；在 src/test/java/com/example/calc/CalculatorTest.java 补两个测试：一个实际调用 Calculator.add(Integer.MAX_VALUE, 1) 并断言抛 ArithmeticException，另一个实际调用 Calculator.add(Integer.MIN_VALUE, -1) 并断言抛 ArithmeticException。不要把 Integer.MAX_VALUE + 1 或 Integer.MIN_VALUE - 1 写成传入方法前就完成的参数表达式；不要执行 git commit，只改这两个指定文件。
```

The two existing tests (`addsTwoPositiveNumbers` → `addExact(2,3)==5`, `addsNegativeNumbers` → `addExact(-1,-1)==-2`) still pass under `Math.addExact`, so no test conflict. The fixture already contains `Calculator.add` + `CalculatorTest`; **no fixture change is needed** for M1.9.

## 4. Components

### 4.1 `scripts/qualify-m1.sh` (new) — the acceptance script

```
startup:
  set -euo pipefail
  cd "$(dirname "${BASH_SOURCE[0]}")/.."
  skip() { echo "SKIP: $*"; exit 0; }
  stop_client() {
    if [ -n "${CLIENT_PID:-}" ] && kill -0 "$CLIENT_PID" 2>/dev/null; then
      kill -TERM "$CLIENT_PID" 2>/dev/null || true
      wait "$CLIENT_PID" 2>/dev/null || true
    fi
    CLIENT_PID=""
  }
  fail() {
    echo "FAIL: $*" >&2
    stop_client
    if [ -n "${DATA:-}" ] && [ -f "$DATA/kernel.log" ]; then
      tail -n 40 "$DATA/kernel.log" >&2 || true
    fi
    if [ -n "${DATA:-}" ] && [ -f "$DATA/wf.out" ]; then
      echo "--- workflow client output (last 80 lines) ---" >&2
      tail -n 80 "$DATA/wf.out" >&2 || true
    fi
    exit 1
  }
  trap 'stop_client; exit 130' INT TERM

preconditions (each → SKIP exit 0, no PASS/RESULT/tag if unmet):
  [ "${VIBE_REAL_PROVIDER:-}" = codex ] || skip "set VIBE_REAL_PROVIDER=codex"
  [ -n "${M19_QUAL_BASE:-}" ] || skip "M19_QUAL_BASE is not set"
  if ! QUAL_BASE="$(git rev-parse --verify "${M19_QUAL_BASE}^{commit}")"; then
    skip "M19_QUAL_BASE is not a commit in this repository"
  fi
  git merge-base --is-ancestor "$QUAL_BASE" HEAD >/dev/null 2>&1 \
    || skip "M19_QUAL_BASE is not an ancestor of HEAD"
  for tool in bash git python3 go codex mvn java; do
    command -v "$tool" >/dev/null 2>&1 || skip "$tool is not on PATH"
  done
  GO_VERSION="$(go version)" || skip "go version failed"
  CODEX_VERSION="$(codex --version 2>&1)" || skip "codex --version failed"
  MVN_VERSION="$(mvn -v)" || skip "mvn -v failed"
  JAVA_VERSION="$(java -version 2>&1)" || skip "java -version failed"
  case "$JAVA_VERSION" in
    *'version "1.8.'*) ;;
    *) skip "Java 8 is required (java -version: $JAVA_VERSION)" ;;
  esac
  case "$MVN_VERSION" in
    *'Java version: 1.8.'*|*'Java version: "1.8.'*) ;;
    *) skip "Maven is not running on Java 8 (mvn -v: $MVN_VERSION)" ;;
  esac
  case "$MVN_VERSION" in
    'Apache Maven 3.9.12'*) ;;
    *) skip "Maven 3.9.12 is required (mvn -v: $MVN_VERSION)" ;;
  esac
  INITIAL_STATUS="$(git status --porcelain --untracked-files=all)" \
    || skip "git status failed"
  case "$INITIAL_STATUS" in
    "") ;;
    *) skip "repository must be clean before qualification starts" ;;
  esac
  if [ -z "${M19_RESULT_PATH:-}" ] && [ -e "docs/M1-RESULT.md" ]; then
    skip "canonical docs/M1-RESULT.md already exists; start production qualification from the implementation baseline"
  fi
  if [ -n "${M19_RESULT_PATH:-}" ]; then
    [ -f "docs/M1-RESULT.md" ] || skip "verification run requires the committed canonical docs/M1-RESULT.md"
    git ls-files --error-unmatch "docs/M1-RESULT.md" >/dev/null 2>&1 \
      || skip "verification run requires docs/M1-RESULT.md to be tracked"
    CANONICAL_RESULT_SHA256_BEFORE="$(python3 -c 'import hashlib,sys; print(hashlib.sha256(open(sys.argv[1],"rb").read()).hexdigest())' "docs/M1-RESULT.md")" \
      || skip "cannot hash the canonical docs/M1-RESULT.md"
  fi
  if [ -z "${M19_RESULT_PATH:-}" ]; then
    python3 -c 'import sys; rows=[line for line in open(sys.argv[1],encoding="utf-8") if line.startswith("M1.9 ")]; assert len(rows)==1 and "— in-progress" in rows[0]' "docs/M1-DESIGN.md" \
      || skip "production run requires the M1.9 §13 row to be explicitly in-progress"
  else
    python3 -c 'import sys; rows=[line for line in open(sys.argv[1],encoding="utf-8") if line.startswith("M1.9 ")]; assert len(rows)==1 and "— done" in rows[0]' "docs/M1-DESIGN.md" \
      || skip "verification run requires the committed M1.9 §13 row to be done"
  fi

source "scripts/lib/kernel-harness.sh"     # DATA, SOCK, tokens, restart_kernel, EXIT trap

full-suite gate (must pass before the real run is even attempted; §8):
  define `run_gate <label> <argv...>` that captures combined stdout/stderr and
  the exit code under `set +e`, restores `set -e`, appends the labelled result
  to `$DATA/full-suite.log` for the later evidence block, prints the captured
  output on failure, and calls `fail "<label> (exit <code>)"`; every command
  below goes through this helper. A failed regression is therefore a FAIL with
  the harness diagnostic, not a shell exit that bypasses `fail`. Exact commands:
    run_gate "script syntax: qualify" bash -n "scripts/qualify-m1.sh"
    run_gate "script syntax: projection library" bash -n "scripts/lib/console-projection.sh"
    run_gate "root build" bash "scripts/build.sh"
    run_gate "plugins tests" bash -c 'cd "plugins" && go test "./..."'
    run_gate "cli tests" bash -c 'cd "cli" && go test "./..."'
    run_gate "kernel tests" bash -c 'cd "kernel" && go test "./..."'
    run_gate "M0.5 qualification" bash -c 'cd "kernel" && bash "scripts/build.sh" >/dev/null && python3 "tests/integration/m05_qualification.py"'
    run_gate "architecture checks" bash "scripts/check-arch.sh"
    for i in 1 2 3 4 5; do
      run_gate "smoke run $i" bash "scripts/smoke.sh"
    done
    for i in 1 2 3; do
      run_gate "DONE-integrity run $i" bash "scripts/qualify-done-integrity.sh"
    done
      ← the second loop IS G4; it is not repeated later

setup:
  export VIBE_AGENT_PROVIDERS=codex        # BEFORE restart_kernel, so discovery registers codex
  RESULT_PATH="${M19_RESULT_PATH:-docs/M1-RESULT.md}"
  # RESULT_PATH is either the canonical in-repository result or an absolute
  # path outside this repository for the independent verification rerun.
  # The G1 whitelist remains hard-coded to the canonical four paths.
  if [ -n "${M19_RESULT_PATH:-}" ]; then
    case "$M19_RESULT_PATH" in
      /*) ;;
      *) fail "M19_RESULT_PATH override must be an absolute path outside the repository" ;;
    esac
    RESULT_REALPATH="$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$M19_RESULT_PATH")" \
      || fail "cannot resolve M19_RESULT_PATH"
    REPO_REALPATH="$(pwd -P)" || fail "cannot resolve repository path"
    case "$RESULT_REALPATH" in
      "$REPO_REALPATH"|"$REPO_REALPATH"/*) fail "M19_RESULT_PATH override must not resolve inside the repository" ;;
    esac
  fi
  build_bins
  restart_kernel
  mkdir -p "$DATA/projections" "$DATA/projection-snapshots"
  SRC="$DATA/src" ; mkdir -p "$SRC"
  cp -R "fixtures/sample-java-project/." "$SRC/"
  NESTED_GIT="$(find "$SRC" -mindepth 1 -name .git -print -quit)"
  case "$NESTED_GIT" in
    "") ;;
    *) fail "sample fixture unexpectedly contains a nested .git directory: $NESTED_GIT" ;;
  esac
  printf '%s\n' 'target/' > "$SRC/.gitignore"
  ( cd "$SRC" && git -c init.defaultBranch=main init -q )
  git -C "$SRC" add -A
  git -C "$SRC" -c user.email=qualification@example.invalid \
    -c user.name=m1.9 -c commit.gpgsign=false commit -q -m baseline
  SRC_BASE="$(git -C "$SRC" rev-parse HEAD)"
  [ "$QUAL_BASE" != "$SRC_BASE" ] || fail "qualification and scratch-repository base commits unexpectedly match"
  SRC_STATUS="$(git -C "$SRC" status --porcelain --untracked-files=all)"
  case "$SRC_STATUS" in
    "") ;;
    *) fail "scratch source is not clean after baseline: $SRC_STATUS" ;;
  esac
  POLICY="$DATA/projection-policy.json"
  write the fixed policy from §4.2 with `repo` set to `$SRC`, export
  `CONSOLE_PROJECTION_POLICY="$POLICY"`, and use this same policy for the
  live assertion and every `file:` mutation copy
  after REVIEW_ID and WORKSPACE_ID are known, write a short-lived
  `$DATA/m1.9-review-handoff.json` containing `SOCK`, `TASK`, `WC`, `RID`,
  `WORKSPACE_ID`, and `workspace_path`. After the client disconnect is
  verified, print its path and the exact m1-dev `review show` / diff-inspection
  / `review decide` commands. The printed decision command is exactly
  `".bin/vibe" -socket "<socket>" -identity "m1-dev" -token "m1-dev-token" review decide "<review-id>" -approved -reviewer "m1-dev" -acceptance AC1=pass -acceptance AC2=pass -acceptance AC3=pass`.
  The printed read-only inspection commands are exactly:
  `".bin/vibe" -socket "<socket>" -identity "m1-dev" -token "m1-dev-token" review show "<review-id>" -json`
  followed by
  `".bin/vibe" -socket "<socket>" -identity "m1-dev" -token "m1-dev-token" artifact show "<diff-artifact-id>" -json`
  and
  `git -C "<workspace-path>" diff HEAD -- "src/main/java/com/example/calc/Calculator.java" "src/test/java/com/example/calc/CalculatorTest.java"`.
  The reviewer must compare the shown `diff_artifact_id`, the artifact's
  `blob_uri`/summary, and the inspected worktree with the later
  `artifact.query` projection; the script itself does not decide the review.
  Do not put a
  token in the handoff file; the fixed `DEV_TOKEN` is supplied on the command
  line.
  The handoff JSON keys are exactly `socket`, `task_id`, `work_context_id`,
  `review_id`, `workspace_id`, and `workspace_path`; its values are the
  captured runtime values, not placeholders.

task setup (as m1-dev, repo is the harness-created $SRC, not the fixture path):
  vibe task create -title "M1.9 real overflow hardening" \
    -goal "Use Math.addExact in Calculator.add and cover positive and negative integer overflow" \
    -repo "$SRC" -scope \
    "src/main/java/com/example/calc/Calculator.java,src/test/java/com/example/calc/CalculatorTest.java" \
    -ac AC1="mvn -q test succeeds" \
    -ac AC2="tests separately call Calculator.add(Integer.MAX_VALUE, 1) and Calculator.add(Integer.MIN_VALUE, -1) and assert ArithmeticException for both" \
    -ac AC3="only the two scoped files are changed"

run (see §5 for the exact WAITING_REVIEW / disconnect protocol; this prompt is
the complete agent instruction because `workflow.engineering.run` does not
automatically inject the Task projection into `agent.run`):
  as local-cli:  vibe workflow run <task> -provider codex \
                   -base-ref "$SRC_BASE" \
                   -prompt "改 src/main/java/com/example/calc/Calculator.java 中的 Calculator.add，用 Math.addExact 让整数溢出自然抛 ArithmeticException；在 src/test/java/com/example/calc/CalculatorTest.java 补两个测试：一个实际调用 Calculator.add(Integer.MAX_VALUE, 1) 并断言抛 ArithmeticException，另一个实际调用 Calculator.add(Integer.MIN_VALUE, -1) 并断言抛 ArithmeticException；不要把 Integer.MAX_VALUE + 1 或 Integer.MIN_VALUE - 1 写成传入方法前就计算完的参数表达式；不要执行 git commit，只改这两个指定文件。" \
                   -build "mvn -q -DskipTests compile"   -test "mvn -q test" \
                   -timeout 30m
  → kill the client at WAITING_REVIEW, verify the workflow survives
  as m1-dev:     resolve review_id (JSON parse) ; while the workspace is still ALLOCATED,
                 call workspace.get{work_context_id}, save its response and WORKSPACE_ID;
                 inspect the diff (resolve diff_artifact_id → artifact.get / blob.get, or
                 read the worktree) ; manually execute review decide … --approved
  independent poll: workflow.engineering.get until stage DONE, then SEALED;
                 also wait for `workspace.get{workspace_id=WORKSPACE_ID}` to
                 report `RELEASED` before restarting or running final gates

gates:   run the G1 kernel/forbidden-path checks, G2..G6 assertions of §7, and
         the projection assertion of §4.2, then complete the D1..D5 sweep of
         §6; each assertion captures output into a variable and matches with
         `case` (never `cmd | grep -q` — pipefail/SIGPIPE flake). Restore a
         green unmodified snapshot after every mutation before proceeding.
         G1's final four-path whitelist is run only after the result write below;
         before that write production mode expects the three implementation paths
         and verification mode expects the already-tracked four paths.

on all green (in this order):
  render the complete result into a temporary file under "$DATA", validate that
  it contains both required verdict lines and no client token/authentication
  material, then atomically move it to "$RESULT_PATH"; if validation or the
  move fails, call `fail` and never print PASS. A partial write must never
  create a result that looks authoritative. In verification mode, before
  proceeding, recompute the SHA256 of the tracked canonical
  `docs/M1-RESULT.md` and require it to equal
  `CANONICAL_RESULT_SHA256_BEFORE`; only the external result may have changed.
  re-run the G1 final whitelist check against the tracked-delta + untracked-file union,
  including the just-written canonical `docs/M1-RESULT.md` when this is the
  production run (not only `git diff <QUAL_BASE> HEAD`, because the result is
  not committed yet); with an external `M19_RESULT_PATH`, the canonical result
  must already be present and the external file is not part of the repository
  whitelist
  print "M1 ENGINEERING VERTICAL SLICE: PASSED"
  # This line is the run-level result only. The M1 milestone is not final until
  # the operator performs the four-file commit, the independent verification
  # rerun, and the final tag below.
  if this is the production run (the default RESULT_PATH), print the post-pass
  operator sequence: change the explicit §13 `in-progress` marker to `done`, then run
  `git add "scripts/qualify-m1.sh" "scripts/lib/console-projection.sh" \
    "docs/M1-RESULT.md" "docs/M1-DESIGN.md" && \
    git commit -m "[M1资格][chore][记录M1.9验收结果]"`, then require
  `git status --porcelain --untracked-files=all` to be empty before the
  independent verification rerun.
  Do not tag yet: the tag is created only after the independent verification
  rerun. If this is the external-result verification run, print
  `git tag -a "m1.9-qualification" -m "M1.9资格验收"` for the reviewer to run
  on the already committed final commit.
  (the script does NOT create a commit or tag itself — both remain manual reviewer steps,
   same as every prior milestone)
```

Structural notes:
- **build/test commands are fixed and relative.** `tool-runner` sets the child's working directory to the allocated workspace (`plugins/tool-runner/runner.go:24`), and `workspace.allocate` happens *inside* the workflow — the script never knows the worktree path when it starts `vibe workflow run`. So the commands are `mvn -q -DskipTests compile` / `mvn -q test` with no `-f`; `pom.xml` resolves from the worktree cwd.
- `.query` capabilities are not friendly `vibe` subcommands; the qualification code uses `.bin/vibe-raw -cap <name> -kind query -service <svc> -authority <auth> -payload …`, exactly as `scripts/verify-real-provider.sh` already does for `agent.run.query` / `blob.get`. The implementation must use this fixed service/authority map (the values are not environment-configurable): `work.get → default-work-registry/work-main`, `workspace.get → default-workspace/workspace-main`, `agent.run.query → default-agent-harness/agent-runs-main`, `artifact.query → default-artifact/artifact-main`, `tool.run.query → default-tool-runner/toolruns-main`, `review.query → default-review/reviews-main`, `session.query → default-session/sessions-main`, and `blob.get → default-blob/blob-main`. The human handoff may additionally use the existing public `artifact.get → default-artifact/artifact-main`; the projection library may not call `artifact.get` or `blob.get`.
- The exact raw-query payloads are fixed: `work.get`=`{"task_id":"$TASK"}`; pre-release `workspace.get`=`{"work_context_id":"$WC"}`; post-release `workspace.get`=`{"workspace_id":"$WORKSPACE_ID"}`; every other projection query=`{"work_context_id":"$WC"}`; `blob.get`=`{"uri":"$URI"}`. No query is made with both selectors, and no query result is synthesized from another query's fields.
- Snapshot filenames are fixed and capability-scoped: `work.get.json`, `workspace.get.json`, `agent.run.query.json`, `artifact.query.json`, `tool.run.query.json`, `review.query.json`, and `session.query.json`. The post-restart seven files live in the empty `$DATA/projection-snapshots` directory; the pre-release bridge response is saved separately as `$DATA/projections/workspace.pre-release.json` (auxiliary evidence, not one of the seven mutation inputs). The post-restart `workspace.get.json` is queried by the captured `WORKSPACE_ID`; the other six are queried by the `work.get` response's `work_context_id`.
- The raw-query wrapper retries only transport/temporary-runtime failures for a bounded 30 seconds after `restart_kernel`; once a capability answers, a semantic `NOT_FOUND`/malformed response is a hard FAIL and is never converted to a retry or PASS.
- `fixtures/sample-java-project` is copied into a throwaway `$SRC`; the runtime `.gitignore` for `target/` is committed in `SRC_BASE`, so Maven output cannot pollute either the artifact scope or `RecoveryCheckpoint.untracked_manifest`.
- codex's first real run may take 1–3 min; `-timeout 30m` and a wall-clock deadline in §5 accommodate it. A fixed number of polls is not sufficient because each CLI query has its own socket timeout.

### 4.2 `scripts/lib/console-projection.sh` (new) — reusable assertion library

A sourced library, not an executable. One entry point:

```
assert_console_projection <mode> <task_id> [workspace_id]
    mode = "live"     → fetch the 7 projections from the running kernel;
                         workspace_id is required and was captured by the public
                         workspace.get{work_context_id} query while ALLOCATED;
                         CONSOLE_PROJECTION_SNAPSHOT_DIR is required and receives
                         the seven raw response payloads under the fixed filenames
    mode = "file:DIR" → load the 7 projections from DIR/<capability>.json (for 致残 —
                        the caller fetches once, mutates a copy, points the library
                        at the copy)
```

The library is source-safe: it defines no top-level side effect beyond helper
definitions — in particular it must not change shell options, `cd`, `export`,
or install/replace a trap — and must never call `exit`. `assert_console_projection` reports a
failed assertion with a specific message and `return 1`; callers invoke it in
an explicit `if`/`case` context. This is required because D1–D3 intentionally
expect the helper to return non-zero and then need to restore the unmodified
snapshot before continuing the sweep. Every query, JSON parse, and file read
inside the function must itself be guarded by `if`/`case` (or an equivalent
status capture), so a caller's `set -e` cannot terminate the shell before the
library returns its specific non-zero result.
All library-private functions and variables use a `projection_` prefix; in
particular it must not define a generic `fail`, `DATA`, `SOCK`, or `POLICY`
symbol that could overwrite the qualification script's state or diagnostic
helper when sourced.

The caller must set `CONSOLE_PROJECTION_POLICY` to a JSON policy file. This is
expected input, not a projection or a source of observed fields, and makes the
same library usable for any M1-shaped `DONE` task (including mock runs). Its
schema is fixed:

```json
{
  "repo": "/absolute/path/to/the/task/repository",
  "title": "M1.9 real overflow hardening",
  "goal": "Use Math.addExact in Calculator.add and cover positive and negative integer overflow",
  "acceptance_criteria": [
    {"id": "AC1", "text": "mvn -q test succeeds"},
    {"id": "AC2", "text": "tests separately call Calculator.add(Integer.MAX_VALUE, 1) and Calculator.add(Integer.MIN_VALUE, -1) and assert ArithmeticException for both"},
    {"id": "AC3", "text": "only the two scoped files are changed"}
  ],
  "provider": "codex",
  "reviewer": "m1-dev",
  "files": [
    "src/main/java/com/example/calc/Calculator.java",
    "src/test/java/com/example/calc/CalculatorTest.java"
  ],
  "tools": {
    "build": ["mvn", "-q", "-DskipTests", "compile"],
    "test": ["mvn", "-q", "test"]
  }
}
```

The policy's `title`, `goal`, `acceptance_criteria`, `provider`, `reviewer`,
`files`, and `tools` are all required; `tools` has exactly the `build` and `test`
keys. Acceptance-criterion ids and texts are compared in exact order, while
`files` is compared as an exact set with duplicate entries rejected; each tool
array is compared in exact argv order; the task's `scope` is compared to
`join(files, ",")` in the policy's declared order. A caller that wants to
run the helper against another task supplies that task's own repository,
provider, scoped files, tool argv, and acceptance-criterion id/text pairs. Both `live` and `file:`
mode use the same policy file; the mutation sweep never edits it.

The qualification script writes this policy with a JSON encoder from the fixed
values, rather than interpolating a path into a heredoc. It rejects an empty,
absolute, or `..`-containing scoped path and requires `repo` to be the absolute
scratch-repository path. This keeps a policy from accidentally becoming an
observed-field backdoor or a path-escaping input.

Navigation mirrors the Console's first lookup: `work.get {task_id}` returns both the
Task and its WorkContext, so the `work_context_id` for the other queries comes from
that response. The released-workspace exception is explicit: `workspace.get` cannot
select a released workspace by `work_context_id` in the current implementation, so
the harness captures the public workspace id before release and reuses that selector
after restart. The library never accepts a pre-resolved `work_context_id`, reads a
journal event to discover the id, or reads private state. In `live` mode the
caller supplies only the already-captured public `workspace_id` in addition to
the task id; the helper still derives the wc from `work.get`. It fetches / loads
exactly these seven capabilities and nothing else:

| projection | capability | key |
|---|---|---|
| task view | `work.get` | `{task_id}` → yields `work_context_id` |
| workspace | `workspace.get` | `{workspace_id}` from the pre-release public response |
| agent runs | `agent.run.query` | `{work_context_id}` |
| artifacts | `artifact.query` | `{work_context_id}` |
| tool runs | `tool.run.query` | `{work_context_id}` |
| reviews | `review.query` | `{work_context_id}` |
| sessions | `session.query` | `{work_context_id}` |

In `live` mode the caller creates an empty
`CONSOLE_PROJECTION_SNAPSHOT_DIR`; the library writes each successful raw
response payload to its capability-scoped filename before asserting it. A
transport failure or malformed response returns non-zero and leaves the run
failed; it must not write a synthetic snapshot. `file:` mode reads only the
seven fixed files from the supplied directory. The qualification harness uses
one live call after restart and reuses those files for D1-D3, so the mutation
sweep cannot accidentally re-query or replace live state.

Assertions (all must hold; any failure → non-zero + a specific message):

1. **IDE-lens fields present**: the returned Workspace has the captured `workspace_id`, matching `work_context_id`, `repo == policy.repo`, non-empty `path`, `base_commit`, `status == "RELEASED"`, and `release_policy == "preserve"` after the final run; `artifact.query` returns exactly one artifact, its `work_context_id ==` the wc, and it is `Artifact{kind=diff}` with `summary.files_changed == len(policy.files)`; `summary.files[]` is **non-empty** and contains exactly the policy's repo-relative scoped paths — no out-of-scope file.
2. **Agent-lens fields present**: exactly one `AgentRun` for this wc with `{id, work_context_id, workspace_path, provider, status, raw_session_ref, provider_metadata}` all populated; `work_context_id ==` the wc, `workspace_path == workspace.path`, `provider == policy.provider`, `status == "COMPLETED"`, `frame_count > 0`, `provider_metadata` has exactly the keys `provider` and `exit_code`, `provider_metadata.provider == policy.provider`, and `provider_metadata.exit_code == 0`. The metadata must contain no argv, prompt, token, or other unredacted process data.
3. **Truth chain wired**: `work.get` returns the expected task with `id == task_id`, `work_context_id ==` the wc, `title == policy.title`, `goal == policy.goal`, `scope == join(policy.files, ",")`, `status == "DONE"`, and its acceptance-criterion id/text pairs exactly equal the policy's `acceptance_criteria` in order. Its WorkContext has `id ==` the wc, `task_id ==` the task id, `repo == policy.repo`, and exactly `len(policy.tools)` EvidenceRefs total, all non-invalidated, with exactly one for each policy tool label, each with `source_capability == "tool.run@1"`, `outcome == "PASS"`, and `source_id` equal to the corresponding ToolRun id. `tool.run.query` returns exactly the policy's ToolRuns with `work_context_id ==` the wc, `workspace_path == workspace.path`, `cwd == workspace.path`, labels exactly `build`/`test`, and JSON field `command[]` (the structured argv) exactly equal to `policy.tools.build` / `policy.tools.test`; `exit_code == 0` and `outcome == "PASS"`. `review.query` returns exactly one Review with `work_context_id ==` the wc, `agent_run_id ==` the AgentRun id, `reviewer == policy.reviewer`, `status == "APPROVED"`, and `diff_artifact_id ==` the `Artifact{kind=diff}.id`; its two-item `evidence_snapshot` has one item per policy tool label, each `outcome == "PASS"`, and each `evidence_ref_id` equals the corresponding ToolRun id (the shipped wiring documented in invariant 11). Its acceptance-result IDs are exactly the policy acceptance-criterion ids with no duplicates, and all `satisfied == true`.
4. **Session record present and internally consistent**: `session.query` returns exactly one `SessionRecord` with non-empty `id`, `archive_ref`, and `archive_hash`, outer `work_context_id ==` the wc, and outer `agent_run_id ==` the AgentRun id. Its outer `event_selection` and `RecoveryCheckpoint.canonical_event_selection` agree on correlation id, event ids, hashes, and count; the correlation id is the wc, and the count satisfies `event_count == len(event_ids) == len(event_sha256s)` (the count may be zero under invariant 8). The `RecoveryCheckpoint` has `work_context_id ==` the wc, `agent_run_id ==` the AgentRun id, `worktree_path_at_seal == workspace.path`, `base_commit == head_commit == workspace.base_commit`, `branch == workspace.branch`, `dirty == true`, `untracked_manifest == []`, and non-empty `tracked_patch_ref`; under the current zero-plugin-change flow its `task_id`, `provider`, `diff_artifact_id`, and `harness_native_id` are explicitly empty. The live G5 blob check separately parses the archive, requires its nested `session_record.id`/`work_context_id`/`agent_run_id` and the entire nested `recovery_checkpoint` to agree with the public checkpoint, requires the nested `session_record.archive_ref` and `archive_hash` to be empty strings (they were serialized before those fields were filled), requires each nested canonical event's `id`/`sha256` to match the selection arrays in order, and requires `canonical_events` length to equal that event count; the `file:` mode remains self-contained with only the seven projection JSON files.
5. **View is reconstructable and consistent across projections**: taken together, the 7 projections yield one coherent WorkContext view — the task, wc, released workspace, codex AgentRun, two ToolRuns/EvidenceRefs, diff Artifact, Review, and sealed SessionRecord all join by their public ids and agree on the two scoped files. This is the **conditional** ADR-002 assertion: the public projection set is sufficient once the public `workspace_id` captured during the allocated lifetime is retained; it does not certify task-only rediscovery of an already-released workspace by `task_id` or `work_context_id`.
6. **No private reads / no backfill**: the library contains no path into a plugin data dir, no `git` call, and no `blob.get` call. (Enforced by review of the library source, and by the `file:` mode working with only the 7 JSON blobs plus the caller-supplied policy file. Blob bytes and the preserved worktree are checked by the acceptance harness outside the projection library.)

### 4.3 `docs/M1-DESIGN.md` edits (part of this milestone)

- **§6/§7/§9**: reconcile the existing projection and correlation wording with the shipped M1.6 call path: the Review snapshot's `evidence_ref_id` currently contains the ToolRun id because `AttachEvidence` returns only an error; M1.9 checks the actual WorkContext EvidenceRefs separately and does not describe that snapshot field as an EvidenceRef id. Workflow child calls retain the outer request correlation, while the `session.seal` payload includes neither `correlation_id` nor `event_ids`; replace the claim that workflow event ids are handed to `session.seal` with the actual behavior and state that M1.9 checks canonical-event selection shape/archive consistency and permits `event_count == 0`, rather than claiming a non-empty event selection that the current zero-plugin flow does not guarantee.
- **§10**: replace the task text with §3 above and replace the run snippet with the exact prompt/build/test arguments and WAITING_REVIEW client-disconnect protocol from §4.1/§5; state that `fixtures/sample-java-project` is copied into a harness-created clean `$SRC` Git repository (the fixture directory itself is not passed as `--repo`); replace `vibe review show <task-id>` (wrong — the CLI's `review show` takes a `<review-id>`, `cli/vibe/main.go:839`) with the two-step *"take `review_id` from `vibe workflow show <task-id> -json`, then `vibe review show <review-id>`; inspect the diff via `diff_artifact_id` → `artifact.get`/`blob.get` or the worktree"*; note codex-must-not-commit; note the checkpoint carries wc/agent_run/patch-ref but not task/provider/diff-artifact under the M1 flow (invariant 8), and that its canonical-event selection may be empty; note that post-release Workspace lookup uses the public `workspace_id` captured while allocated and that `agent runtime` means the post-DONE agent-harness runtime restart, not recovery of an in-flight codex process. State explicitly that the read-projection conclusion is conditional on retaining that public workspace id; M1.9 does not close task-only navigation for an already-released workspace.
- **§2**: expand each of G1–G6 with the concrete assertion from §7 (currently one-line criteria).
- **§13**: the implementation commit must make the M1.9 row's terminal marker
  explicitly `in-progress` (the current row has no terminal marker); only after
  the real run and final whitelist pass may the operator change that exact marker
  to `done` and add only the prescribed §13 result summary: point to
  `docs/M1-RESULT.md` for the recorded "M1 PASSED" line and retain the
  conditional ADR-002 wording. This marker transition is part of the four-file result
  commit, never an implementation-side success default.

The exact §13 line sequence is pinned here so the implementation and result
commit cannot invent a second completion meaning:

```text
implementation commit:
M1.9  完整 qualification（§10）+ kill runtime + restart kernel + recovery 验证 + Console 读投影充分性验收（§10，证伪 ADR-002 的"无需返工"结论）→ G1–G6 全过 → M1 PASSED — in-progress

result commit:
M1.9  完整 qualification（§10）+ kill runtime + restart kernel + recovery 验证 + Console 读投影充分性验收（§10，证伪 ADR-002 的"无需返工"结论）→ G1–G6 全过 → M1 PASSED — done；result: docs/M1-RESULT.md；ADR-002 read-projection result: CONDITIONAL — public workspace_id bridge verified；task-only released-workspace navigation remains open
```

Only the terminal marker and the prescribed result suffix change between the
two lines; the result suffix must not claim unconditional ADR-002 closure.

### 4.4 `docs/M1-RESULT.md` (new, produced by the run)

Written **only** when `qualify-m1.sh` reaches all-green. Contains: `QUAL_BASE`, `SRC_BASE`, `WORKSPACE_ID`, the run's real ids (task, wc, agent_run, diff artifact, both tool runs, review, session), the codex/Maven/Java/Go versions, the six G-gate evidence blocks with actual values, the D1–D4 expected-red/restore evidence plus the D5 G4 evidence, the observed canonical-event count (including the permitted zero), the Review snapshot identity limitation, the public projection selector limitation, and two explicit verdicts: `M1 ENGINEERING VERTICAL SLICE: PASSED` and `ADR-002 read-projection result: CONDITIONAL — public-id bridge verified; task-only released-workspace navigation remains open`. The transient `$DATA/full-suite.log` is not copied verbatim; the renderer copies only fixed gate labels, exit statuses, and the required result facts. It must contain no client token, authentication material, or raw prompt/transcript bytes. This file is the durable proof of the M1 qualification result; it must not say that the unqualified ADR-002 "无需返工" claim is closed. A verification rerun supplies `M19_RESULT_PATH` pointing outside the repository and must not overwrite the committed canonical result.

## 5. WAITING_REVIEW / client-disconnect protocol

```
1. start the client in its own process so the pid IS the vibe process:
     capture one absolute 30-minute wall-clock deadline immediately before
     starting the client; every later poll in steps 2, 6, and 7 uses the
     remaining time to this same deadline (never a freshly reset 30-minute
     window):
     PROMPT="改 src/main/java/com/example/calc/Calculator.java 中的 Calculator.add，用 Math.addExact 让整数溢出自然抛 ArithmeticException；在 src/test/java/com/example/calc/CalculatorTest.java 补两个测试：一个实际调用 Calculator.add(Integer.MAX_VALUE, 1) 并断言抛 ArithmeticException，另一个实际调用 Calculator.add(Integer.MIN_VALUE, -1) 并断言抛 ArithmeticException；不要把 Integer.MAX_VALUE + 1 或 Integer.MIN_VALUE - 1 写成传入方法前就计算完的参数表达式；不要执行 git commit，只改这两个指定文件。"
     ( exec ".bin/vibe" -socket "$SOCK" -identity local-cli -token "$TOKEN" \
         workflow run "$TASK" -provider codex \
         -base-ref "$SRC_BASE" \
         -prompt "$PROMPT" \
         -build "mvn -q -DskipTests compile" -test "mvn -q test" -timeout 30m \
     ) >"$DATA/wf.out" 2>&1 &
     CLIENT_PID=$!
2. poll `vibe workflow show <task>` until "stage WAITING_REVIEW".
   Use the remaining time to the step-1 deadline (not a fixed 2-minute or
   fixed-poll bound), because every CLI query has its own socket timeout and
   codex + Maven can take several minutes. If CLIENT_PID exits before
   WAITING_REVIEW is seen → FAIL, dump wf.out.
3. while the workflow is still WAITING_REVIEW and before any decision, as m1-dev:
     call public `workspace.get{work_context_id}`; require exactly one
     `ALLOCATED` Workspace whose `work_context_id`, `repo`, and
     `base_commit` agree with the WorkContext and `SRC_BASE`; require a
     non-empty `path`; save the full response as
     `$DATA/projections/workspace.pre-release.json` and its `workspace_id` as
     WORKSPACE_ID. This is the only source of the later workspace selector.
4. REVIEW_ID: `vibe workflow show <task> -json` piped to a python3 one-liner
   that loads `events[]`, selects the one event whose `type` is
   `review.requested` and whose payload has the current `task_id` and `work_context_id`,
   reads `event.payload.review_id`, requires exactly one non-empty id, and prints
   it (NOT grep — SIGPIPE/pipefail, the M1.5/M1.6 flake). The pinned extraction
   is equivalent to:
   `python3 -c 'import json,sys; d,task,wc=json.load(sys.stdin),sys.argv[1],sys.argv[2]; ids=[e.get("payload",{}).get("review_id") for e in d.get("events",[]) if e.get("type")=="review.requested" and e.get("payload",{}).get("task_id")==task and e.get("payload",{}).get("work_context_id")==wc]; assert len(ids)==1 and ids[0]; print(ids[0])' "$TASK" "$WC"`.
   Before printing the handoff, query public `review.query{work_context_id}`
   and require exactly one review whose id is RID and whose status is `PENDING`.
   This records that the review was pending before the second terminal could
   decide it. Write the handoff file after this step, but do not print the
   decision command until the client disconnect in step 5 has been verified.
   The qualification script may use the m1-dev identity for the read-only
   pre-release workspace capture, but only the human's second-terminal
   m1-dev action may call `review decide`.
5. client disconnect:
     set +e; kill -TERM "$CLIENT_PID" 2>"$DATA/kill-client.err"; kill_rc=$?; set -e
     case "$kill_rc" in
       0) : ;;
       *) fail "client was not alive at disconnect time: $(cat "$DATA/kill-client.err")" ;;
     esac
     set +e; wait "$CLIENT_PID"; rc=$?; set -e
     # SIGTERM to a process blocked on the socket read → 143 (or 130 for SIGINT).
     # rc==0 would mean the client finished on its own → we did NOT test a disconnect.
     case "$rc" in
       143|130) CLIENT_PID="" ;;
       0) fail "client exited 0 — no disconnect was tested" ;;
       *) fail "unexpected client exit $rc" ;;
     esac
6. as m1-dev (a different identity/token — the "second terminal"):
     RID from step 4
     only now print/read the handoff file and the exact commands; the client
     disconnect must already have been observed as 143 or 130
     inspect the diff with `review show "$RID" -json` and
       `git -C "<workspace-path>" diff HEAD --
       "src/main/java/com/example/calc/Calculator.java"
       "src/test/java/com/example/calc/CalculatorTest.java"`; compare the
       displayed `diff_artifact_id` with the worktree diff and eyeball it
     manually execute `vibe review decide "$RID" -approved -reviewer m1-dev -acceptance AC1=pass
       -acceptance AC2=pass -acceptance AC3=pass`.
     The qualification script must not self-approve. After printing the handoff,
     it remains alive and polls public `review.query{work_context_id}` every
     500 ms until the same absolute step-1 deadline; it must continue only
     after finding exactly RID with status `APPROVED`. `CHANGES_REQUESTED`,
     `NOT_FOUND`, a malformed response, or expiry is a FAIL. The poll is a
     read-only synchronization point; no code path in the script may call
     `review.decide`.
7. independent completion check — NOT the killed client's exit code, and run by
   the still-running harness with the m1-dev identity (the second terminal only
   supplies the decision):
     poll `vibe workflow show <task>` (query path = workflow.engineering.get) until
     stage is DONE, then until stage is SEALED; the subsequent seven-projection
     fetch, whose `session.query` is the projection-library call, must return
     exactly one SessionRecord. Separately poll
     `workspace.get{workspace_id=WORKSPACE_ID}` until it reports `RELEASED`.
     `SEALED` is emitted before the workflow's final `workspace.release`, so
     seeing the stage alone is not sufficient.
     Use `min(5 minutes, remaining time to the step-1 deadline)`; five minutes
     is longer than the local `session.seal` child deadline while the shared
     outer bound prevents the script from waiting after the workflow command's
     30-minute deadline has expired.
8. restart_kernel      ← kills kernel + all plugin children, restarts, waits query-ready
9. set `CONSOLE_PROJECTION_SNAPSHOT_DIR="$DATA/projection-snapshots"` and call
   `assert_console_projection live "$TASK" "$WORKSPACE_ID"`; this single live
   call fetches exactly the 7 snapshots, saves them under the fixed filenames,
   and asserts them. It uses WORKSPACE_ID for `workspace.get` and the wc
   returned by `work.get` for the other six. Run §7 G1..G6 + §6 致残 against
   those saved snapshots; do not issue a second live projection fetch.
```

If step 7 never reaches DONE after a valid decide, the workflow **did** get cancelled by the disconnect → invariant 5 fails → M1.9 FAIL.

## 6. 致残 sweep

| # | mutation | expected |
|---|---|---|
| D1 | in the projection `file:` snapshot, set `Artifact{kind=diff}.summary.files` to `[]`, call `assert_console_projection file:<dir> <task_id>` | FAIL at assertion 1 ("summary.files empty") |
| D2 | in the snapshot, change the `AgentRun.provider` (`agent.run.query` result) to `"mock"`, call the helper with the unchanged M1.9 policy (`provider == codex`) | FAIL at assertion 2 ("provider does not match policy") |
| D3 | in the snapshot, point `Review.diff_artifact_id` at a non-existent id | FAIL at assertion 3 ("review does not reference the diff artifact") |
| D4 | first run the unmodified G6 patch-apply check and record it green; take the blob behind `RecoveryCheckpoint.tracked_patch_ref`, require the first hunk to contain a context line, and in a temporary copy replace that context line's payload with a sentinel line that cannot occur in **SRC** while preserving the leading context marker and newline; run `git apply --check` against a clean checkout of **SRC** at `base_commit` | the unmodified check is green; the deliberately impossible context makes `git apply --check` exit non-zero (proves G6 actually applies the patch, not just reads the ref, without relying on Git's offset/fuzz behavior); the original check is green again after the temporary copy is removed |
| D5 | no additional mutation: retain the full-suite `scripts/qualify-done-integrity.sh` ×3 output as the G4/D5 evidence; its own M-S1..M-S4 sweep stands | the recorded G4/D5 sub-sweep is green; do not run a fourth copy here |

Each mutation operates on a **copy** of the seven fetched projection JSON files, never on live state. The `workspace.get` snapshot is the post-restart response selected by WORKSPACE_ID. The unchanged `CONSOLE_PROJECTION_POLICY` remains outside the mutation directory. D4 downloads the patch bytes through `blob.get` and corrupts only a temporary local copy by replacing the first hunk's first context line with a sentinel such as ` __M19_IMPOSSIBLE_CONTEXT__` (the leading single space is retained); if no context line exists, D4 fails because this deterministic mutation cannot be applied to this qualification patch. It never overwrites an immutable blob or calls a write capability. Before corrupting D4, the normal G6 patch-apply procedure must pass; after the expected-red check, deleting the temporary copy and rerunning the same normal procedure must pass again. After each D1–D3 negative assertion the mutated directory is discarded and the unmodified snapshot is passed through the helper again; the helper must return green before the next mutation. `qualify-m1.sh` itself only ever reads live state, so a normal run leaves nothing mutated. `git apply --check` in D4 is run as `… || rc=$?` and the non-zero asserted explicitly (invariant 9).

## 7. G1–G6 evidence matrix

| Gate | Assertion (all captured into variables, matched with `case`) |
|---|---|
| **G1** Kernel Purity | `python3 "kernel/architecture-tests/check_boundaries.py"` exits 0; `git diff --name-only "$QUAL_BASE" -- "kernel"` is empty; no path under `plugins/` or `contracts/` and none of the protected scripts is changed. Before the result write, the tracked-delta + untracked-file union is exactly the three implementation paths (`scripts/qualify-m1.sh`, `scripts/lib/console-projection.sh`, `docs/M1-DESIGN.md`) in production mode, or exactly those three plus the already-tracked `docs/M1-RESULT.md` in verification mode. After the production result is written, or after the external verification result is written, the final union must be exactly `scripts/qualify-m1.sh`, `scripts/lib/console-projection.sh`, `docs/M1-DESIGN.md`, `docs/M1-RESULT.md` — no wildcard, spec, or plan path. After the result is committed, the reviewer repeats the commit-level `git diff --name-only "$QUAL_BASE" HEAD` check. |
| **G2** Real Execution | The M1.9 policy requires `AgentRun.provider == "codex"`; `workspace.status == "RELEASED"`, `workspace.release_policy == "preserve"`, `workspace.base_commit == SRC_BASE`, and the preserved worktree's current `HEAD == SRC_BASE`; it has a non-empty `git diff HEAD`; its changed+untracked path set is exactly the policy's two scoped paths and its untracked set is empty (Maven `target/` is ignored in the SRC baseline); `Artifact{kind=diff}.summary.files[]` matches that set. Outside the projection library, the harness resolves `Artifact.blob_uri` with `blob.get` and byte-compares the patch with the preserved worktree's `git diff HEAD`; that filesystem check never fills a projection field. The harness also reads only those two changed source files and, after whitespace normalization, requires `Calculator.java` to contain `Math.addExact` and `CalculatorTest.java` to contain the two exact overflow calls plus `ArithmeticException`; this is a task-semantic guard, not projection backfill. Codex stdout is **not** consulted. |
| **G3** Truth Chain | The exact join and cardinality checks in §4.2 assertion 3 hold under the M1.9 policy: Task / WorkContext / Workspace / AgentRun / Artifact(diff) / ToolRun(build) / ToolRun(test) / EvidenceRef×2 / Review / SessionRecord all belong to the same wc; every cross-reference resolves to the expected sibling; Review is the approved review for this AgentRun and diff; AC1/AC2/AC3 are each present exactly once and satisfied; build/test argv and outcomes are exact; the Review snapshot has exactly the two ToolRun-id entries required by the shipped M1.6 wiring. |
| **G4** DONE Integrity | the full-suite gate's `bash "scripts/qualify-done-integrity.sh"` ×3 (each ending `DONE-INTEGRITY QUALIFICATION: OK`) **is** G4 — run once, up front. The happy-path DONE of this milestone does **not** substitute for it. |
| **G5** Persistence | After DONE + seal, `restart_kernel` (kernel + all plugin processes killed and restarted). Then re-query all seven projections, using WORKSPACE_ID for `workspace.get` and the wc from `work.get` for the other six. Resolve `AgentRun.raw_session_ref`, `Artifact.blob_uri`, `SessionRecord.archive_ref`, `RecoveryCheckpoint.tracked_patch_ref`, and both ToolRun stdout/stderr URIs via `blob.get`. The first four must resolve to non-empty bytes; tool stdout/stderr blobs must resolve even when a successful command produced zero bytes. Parse the archive's `session_record`, `recovery_checkpoint`, and `canonical_events` members; the nested recovery checkpoint must equal the public checkpoint, nested session identity/event-selection fields must agree with the public SessionRecord, and nested `archive_ref`/`archive_hash` must be empty strings because the nested record was serialized before those fields were filled. Each nested canonical event's `id`/`sha256` must match the selection arrays in order, and `canonical_events` length must equal the checkpoint selection's `event_count`. SHA256 of the archive bytes must equal `SessionRecord.archive_hash`. |
| **G6** Recovery | `RecoveryCheckpoint.tracked_patch_ref` resolves via `blob.get`; a fresh `git clone` of **SRC** (the run's own clone, not `fixtures/sample-java-project` — `base_commit` is a commit in SRC) + `git checkout <base_commit>` + `git apply --check` + `git apply` succeeds; the resulting tracked changed path set is exactly the two scoped files and the untracked set is empty. `RecoveryCheckpoint.{work_context_id, agent_run_id, base_commit}` and `workspace.base_commit` match the projections. Field-non-empty alone is **not** sufficient — the patch must actually apply. |

## 8. Acceptance (what "M1.9 done" means)

"M1 PASSED" is not just this one scenario — it is the whole M1 suite green **plus** the real end-to-end run. In this document that label means the M1 vertical-slice gates passed; it must not be read as an unconditional closure of ADR-002's task-only released-workspace navigation claim.

1. **Full regression green first** (the script gates on this before attempting the real run): `bash "scripts/build.sh"`; `(cd "plugins" && go test "./...")`; `(cd "cli" && go test "./...")`; `(cd "kernel" && go test "./...")`; `(cd "kernel" && bash "scripts/build.sh" >/dev/null && python3 "tests/integration/m05_qualification.py")` → PASSED; `bash "scripts/check-arch.sh"` → `31 contracts` / `10 manifests` / `ARCH CHECKS OK`; `bash "scripts/smoke.sh"` ×5 → `M1 SMOKE: PASSED`, no orphans; `bash "scripts/qualify-done-integrity.sh"` ×3 → `DONE-INTEGRITY QUALIFICATION: OK`.
2. `M19_QUAL_BASE=<dispatch baseline> VIBE_REAL_PROVIDER=codex bash "scripts/qualify-m1.sh"` on the dev machine prints `M1 ENGINEERING VERTICAL SLICE: PASSED`, with G1–G6 evidence in the output. The script must not replace `M19_QUAL_BASE` with the current `HEAD`.
3. The 致残 sweep D1–D4 each reproduce the expected failure, then restore green; G4/D5's sub-sweep passes.
4. The first run writes `docs/M1-RESULT.md` before the PASSED line; that line is only the run-level result, not the final milestone claim. The implementation commit must contain exactly one M1.9 §13 row marked `— in-progress`; the reviewer then changes that marker to `— done` and adds only the prescribed §13 result summary, stages all four allowed paths (`scripts/qualify-m1.sh`, `scripts/lib/console-projection.sh`, `docs/M1-DESIGN.md`, `docs/M1-RESULT.md`), commits them with `[M1资格][chore][记录M1.9验收结果]`, repeats the commit-level G1 check, and requires a clean `git status --porcelain --untracked-files=all`.
5. `docs/M1-DESIGN.md` §2 / §10 / §13 reconciled; §13 is changed to `done` only after the real run succeeds, and the documented ADR-002 result remains explicitly conditional on the retained public `workspace_id`.
6. Reviewer independently re-runs `M19_RESULT_PATH="$(mktemp -t m1.9-result.XXXXXX)" M19_QUAL_BASE=<dispatch baseline> VIBE_REAL_PROVIDER=codex bash "scripts/qualify-m1.sh"` once and re-does the 致残 sweep; the harness hashes the tracked canonical `docs/M1-RESULT.md` before and after and requires the hash to be unchanged (校验者≠生产者).
7. Tag `m1.9-qualification` created on the commit that carries all of the above (manual `git tag`, per the script's printed command), then verified with `git rev-parse "m1.9-qualification^{commit}"` equal to `HEAD`.
8. `git diff --name-only "$QUAL_BASE" HEAD -- "kernel"` empty; no plugin/contract change; the changed-file set matches the exact four paths in G1.

## 9. NON-GOALS

- `work.query@1` / context enumeration — Console v1 (subsystem B), not M1.9.
- Any Console/TUI/GUI code — subsystem B.
- **Enriching the `session.seal` call** so `RecoveryCheckpoint` carries `task_id` / `provider` / `diff_artifact_id` — the session plugin already accepts these; wiring `engineering-workflow` to send them is a ~3-line change but it makes M1.9 no longer zero-plugin-change. Deferred (M2, or a standalone follow-up). M1.9 asserts view consistency from the other projections instead (invariant 8).
- **Changing workspace query semantics** or adding an `active_workspace_ref` write to work-registry — M1.9 keeps the existing plugin behavior. The qualification explicitly captures the public `workspace_id` while allocated and uses that id after release; it does not claim that a post-release Console can rediscover the workspace from `task_id`/`work_context_id` alone.
- **Closing ADR-002 unconditionally** — M1.9 records the seven-projection join as `CONDITIONAL`; if ADR-002 requires task-only navigation of a released workspace, that remains an open product/API decision and cannot be silently converted into a PASS by this harness.
- **Repairing workflow correlation/event-id wiring for session archives** — M1.9 does not modify `engineering-workflow` or `session`; it verifies the current archive shape and permits an empty canonical-event selection. A later milestone may pass `correlation_id`/`event_ids` explicitly and then strengthen this assertion to require selected events.
- **Repairing Review evidence-snapshot identity wiring** — M1.9 does not change the `engineering-workflow` capability shape or make `AttachEvidence` return an id; it records the shipped ToolRun-id snapshot behavior and independently validates the WorkContext EvidenceRefs. A later workflow change may make `evidence_snapshot[].evidence_ref_id` carry the actual EvidenceRef id.
- Auto-resume / reconciler / persistent orchestration state — M2 (§7 of M1-DESIGN).
- Verifying recovery of an in-flight pre-DONE codex subprocess (see invariant 7).
- Structured transcript rendering, interactive agent follow-up — M2 / UI phase.
- Fixing the M1.8 `probeVersion` discovery-test flake — separate follow-up, not this milestone.
- Wiring any of this into `check-arch.sh` / `smoke.sh` / CI.

## 10. Known limitations

- Each qualification execution covers one task. The production result and the independent verification both prove the chain once with a real agent, not that every prompt/agent/repo combination works.
- codex is non-deterministic: a run can fail because codex produced a bad patch or failing tests. That is a *real* negative result (the gate correctly refuses DONE), not a harness bug — re-run. The harness must surface *which* stage failed (agent / build / test / review / gate).
- Java 8 + JUnit 4 fixture; `Math.addExact` is Java 7+, fine.
- Maven downloads plugins on first run; the dev machine must have a warm `~/.m2` or network. Recorded as a precondition, not worked around.
- The current workspace API has no released-by-context selector. The stable-id capture is a qualification bridge, not a new production projection or a claim that the future Console's task-only navigation is solved.
- Consequently, the result has two separate meanings: G1–G6 and the real workflow may be `PASSED`, while the ADR-002 projection conclusion is `CONDITIONAL` until a released workspace can be selected from the Console's intended navigation inputs.
- Under the current M1.6 workflow wiring, the `session.seal` payload carries no `correlation_id`/`event_ids`, so `canonical_event_selection` can be empty even though the archive and checkpoint are durable. M1.9 checks cardinality and archive consistency; selected-event completeness is deferred with the wiring fix.

## 11. Drift guardrails

### Design invariants

- M1.9 must change no kernel/plugin/contract; the qualifying run must use
  codex; the projection library must read exactly the seven public queries;
  `workflow.engineering.get` and `blob.get` are outer-harness-only helpers.
- A mock run or a precondition `SKIP` must never produce a PASS; `QUAL_BASE`
  and the scratch-repository `SRC_BASE` must be resolved independently.
- Projection expectations must come only from the explicit policy file; no
  private state, journal payload, observed response, or filesystem check may
  backfill a projection field. The ADR-002 conclusion must remain conditional.

### Reference inheritance map

| 可继承 | 不可继承 | 原因 |
|---|---|---|
| `kernel-harness.sh` 的构建、重启、清理生命周期 | `smoke-workflow.sh` 的自动审批、mock 写文件、固定短轮询 | M1.9 必须保留人工 Review 和真实 codex |
| `verify-real-provider.sh` 的 codex 门控、raw query、blob 解析习惯 | 直接把 fixture 目录当运行时 repo、把 CLI 文本当投影 | fixture 不是 Git 仓；CLI 文本缺少完整关联字段 |
| `qualify-done-integrity.sh` 作为 G4 的独立对抗回归 | 用 G4 的 mock 运行冒充真实端到端链 | G4 是独立 DONE 闸，不是 M1.9 real-provider 证据 |

### Enum/field ownership

| 名称 | 层 | 含义 | 允许来源 | 不得混淆 |
|---|---|---|---|---|
| `provider` | AgentRun projection | 本次 AgentRun 实际选中的 provider | `agent.run.query` | policy 期望值、codex stdout |
| `diff_artifact_id` | Review projection | Review 绑定的 diff Artifact | `review.query`，再与 `artifact.query` join | `tracked_patch_ref`、任意旧 diff |
| `EvidenceRef.source_id` | WorkContext projection | build/test 事实对应的 ToolRun id | `work.get` | Review snapshot 的 `evidence_ref_id` |
| `Review.evidence_snapshot[].evidence_ref_id` | Review projection | 当前 M1.6 实现写入的 ToolRun id | `review.query` | WorkContext 的 EvidenceRef.id |
| `status` | Task / Workspace / AgentRun / Review / workflow stage | 各自状态机的状态 | 对应 projection；stage 仅由 `workflow.engineering.get` 同步 | 不跨对象复用 `DONE`、`SEALED`、`RELEASED` |
| `RecoveryCheckpoint.*` | Session projection | seal 时的恢复事实；当前流程不填 task/provider/diff | `session.query`及其 archive | 不作为 Task/Agent/Artifact 的身份真源 |
| `QUAL_BASE` / `SRC_BASE` | harness runtime | 主仓实现基线 / scratch repo 基线 | Git 明确解析 | 不能把两个仓库的 HEAD 互换 |

### Hidden-default reuse decisions

| 既有 helper / 默认值 | 决策 | M1.9 约束 |
|---|---|---|
| `kernel-harness.sh` 的 `DATA`/`SOCK`/EXIT trap | safe to reuse | 只能管理本次 harness 生命周期；必须先停止 workflow client 再清理 |
| CLI `workflow run` 的 provider 默认 `mock` | must override | 生产和验证命令都显式传 `-provider codex`，并保留环境门控 |
| CLI `splitArgv` 的空命令默认 `sh -c true` | must not reuse | build/test 必须是固定 Maven argv，不能依赖 CLI 默认值 |
| `workspace.get{work_context_id}` | must wrap | 只在 ALLOCATED 阶段取一次并保存公开 `workspace_id`；RELEASED 阶段只按该 id 查询 |
| `session.seal` 的 payload 默认 correlation=wc、空 event_ids | safe to reuse, but assert explicitly | 不声称事件选择非空；只校验选择数组和 archive 的一致性 |
| `artifact.collect_diff` / `buildCheckpoint` 的默认 base=`HEAD` | safe to reuse | scratch repo 必须先建立并固定 `SRC_BASE`；codex 不得 commit |

### RPC boundary decisions

| 边界 | caller must provide | service derives | internal only |
|---|---|---|---|
| `workflow.engineering.run@1` | `task_id`、prompt、provider、base_ref、固定 build/test argv、deadline | wc、repo、workspace、AgentRun/Artifact/ToolRun/Review/Session ids | delegation、request correlation、journal records |
| 七个 projection query | 一个明确 selector：`task_id`或`workspace_id`或`work_context_id` | 各服务返回自己的 projection，不能从别的 query 拼写字段 | service/authority routing 由 harness 固定，不进 policy |
| `blob.get` | 已由 projection 返回的 URI | blob 内容 | 仅 outer harness 做 byte/hash/apply 校验，library 不调用 |
| `session.seal@1` | 当前 workflow 仅传 wc、agent_run、workspace_path | checkpoint 的 Git/patch/archive 字段 | task/provider/diff/correlation/event ids 不得由 harness 私塞进 projection |

没有新的RPC契约。若未来要求checkpoint携带task/provider/diff-artifact，必须改插件并重新界定M1.9，不能在计划中悄悄扩张范围。

### Implementation gate verdict

**ADMIT with explicit condition:**可以进入writing-plans和实现，但实现只能产生
`scripts/qualify-m1.sh`、`scripts/lib/console-projection.sh`、
`docs/M1-DESIGN.md`、`docs/M1-RESULT.md`四个最终变更路径；必须实现
`in-progress→done`标记转换、生产/验证两次run的结果隔离、七投影精确查询、
G1-G6及D1-D5证据。若产品决策要求ADR-002必须支持“仅凭task_id发现已
RELEASED workspace”，则本门禁立即改为**BLOCKED**，当前零插件变更方案不得宣称M1.9通过。

The implementation plan must pin every `-service`/`-authority`, snapshot filename,
`M19_QUAL_BASE` handoff, result-verification command, and the atomic result-write
failure path.
