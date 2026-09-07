# M1.9 — M1 Qualification (real provider end-to-end + G1–G6) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the two qualification scripts (`scripts/lib/console-projection.sh`, `scripts/qualify-m1.sh`) and reconcile `docs/M1-DESIGN.md`, then run the whole locked engineering workflow on **real codex + real Maven** to `DONE`+`SEALED`, assert G1–G6 with cross-checked evidence, run the D1–D5 致残 sweep, and record `docs/M1-RESULT.md` — the durable proof that M1 passed.

**Architecture:** `console-projection.sh` is a sourced assertion library: given only a `task_id`, it fetches the seven public read-projections (`work.get` by task id, the other six by the `work_context_id` that returns — `workspace.get` included, which after M1.8.5 returns the released workspace), writes them to fixed snapshot filenames, and asserts the reconstructed WorkContext view against a fixed JSON policy file. `qualify-m1.sh` is the dev-machine acceptance harness: precondition/SKIP gate → full regression suite (which includes G4) → scratch `$SRC` repo → `vibe workflow run -provider codex` with the real Java-overflow task → client-disconnect at `WAITING_REVIEW` → human review from a second terminal → independent poll to `SEALED` → `restart_kernel` → one live projection fetch → G1–G6 + D1–D5 against the saved snapshots → atomic `docs/M1-RESULT.md` render. **M1.9 itself changes no kernel, no plugin, no contract** — the workspace-plugin change it depends on was M1.8.5 (`m1.8.5-workspace-by-context-recovery`, merged `4be9939`).

**Tech Stack:** bash (`set -euo pipefail`, `case`-matching, no `cmd | grep -q`); `python3` for every JSON parse; `.bin/vibe` + `.bin/vibe-raw` over the Unix socket; real `codex` (codex-cli 0.152.x) and `mvn` 3.9.12 on Java 8; `scripts/lib/kernel-harness.sh` for kernel lifecycle.

**Spec:** `docs/superpowers/specs/2026-09-03-m1-9-qualification-design.md` (rev12) — the plan argues from the spec; every "implement §X exactly" below points into it. Executors read both.

**Execution model:** **not connector-dispatched** (the air-gapped sandbox has neither codex nor Maven, and §5 needs an interactive human review). Tasks 1–5 are TDD, exercised against a **mock**-produced `DONE` task, and may be done by a local agent session. Tasks 6–7 (the real codex production run, and the independent verification rerun) are done by the operator and then the reviewer on the dev machine — the spec's two-role model (§8).

## Global Constraints

Copied verbatim from the spec's §2 invariants — every task's requirements implicitly include these:

- **No kernel / plugin / contract change.** `git diff "$QUAL_BASE"` touches nothing under `kernel/`, `plugins/`, `contracts/`, and does not modify `scripts/check-arch.sh`, `scripts/smoke.sh`, `scripts/smoke-*.sh`, `scripts/qualify-done-integrity.sh`. The final implementation delta is exactly `scripts/qualify-m1.sh`, `scripts/lib/console-projection.sh`, `docs/M1-DESIGN.md`, `docs/M1-RESULT.md`.
- **Real provider is mandatory** for the qualifying run (`provider=codex`). A run with `provider=mock`, `-mock-write-file`, or `sh -c true` build/test is **not** qualifying and must not print `M1 ENGINEERING VERTICAL SLICE: PASSED`.
- **No SKIP-as-PASS.** Missing `VIBE_REAL_PROVIDER=codex` / `M19_QUAL_BASE` / any of `bash git python3 go codex mvn java` / Java-8 / Maven-3.9.12 / clean tree → `SKIP: …`, `exit 0`, **no** PASS line, **no** `docs/M1-RESULT.md` write, **no** tag.
- **Projection reads only the public query surface** — exactly `work.get`, `workspace.get`, `agent.run.query`, `artifact.query`, `tool.run.query`, `review.query`, `session.query`. No plugin private log/dir, no `git diff` backfill, no `blob.get` inside the library. Every query is keyed by `task_id` or the `work_context_id` it yields; **no `workspace_id` selector anywhere**.
- **致残 mutates data, not tests** — D1–D3 null/alter a field in a **copy** of the seven snapshot JSON files and re-invoke the library; they never delete an assertion or re-run the workflow.
- **Client disconnect must not cancel the workflow** — `qualify-m1.sh` kills the `vibe workflow run` client at `WAITING_REVIEW`, then proves via an independent `workflow.engineering.get` poll (different identity) that it still reaches `DONE`/`SEALED` after the human decides.
- **codex must not commit** — the task prompt forbids `git commit`; `artifact.collect_diff` and `RecoveryCheckpoint.tracked_patch_ref` are built from `git diff HEAD` (uncommitted).
- **"kill agent runtime" = restart the agent-harness plugin runtime** — the codex subprocess is already gone by the time `agent.run` returns; G5 restarts the kernel + all plugin processes *after* DONE+seal.
- **The checkpoint is verified for what it actually carries** — `RecoveryCheckpoint` has `work_context_id`, `agent_run_id`, `base/head_commit`, `branch`, `dirty`, `untracked_manifest`, `tracked_patch_ref`, `canonical_event_selection`; `task_id`/`provider`/`diff_artifact_id`/`harness_native_id` are **empty** (the workflow's `session.seal` call sends only wc/agent_run/workspace_path). Provider/diff/task consistency is asserted from the `AgentRun`/`Review`/`Artifact`/`work.get` projections instead.
- **`canonical_event_selection` may be empty** — the `session.seal` payload carries no `correlation_id`/`event_ids`; assert selection-array/archive cardinality agreement, not non-emptiness.
- **Review snapshot identity** — `Review.evidence_snapshot[].evidence_ref_id` holds the **ToolRun id** (the shipped M1.6 wiring, because `AttachEvidence` returns only an error); the real WorkContext `EvidenceRef` ids are checked separately via `EvidenceRef.source_id`.
- **Shell hygiene** — `set -euo pipefail`; capture into a variable and match with `case`; env guards `${VAR:-}`; any expected-non-zero command (`kill`/`wait` → 143/130, `vibe workflow run` non-DONE exit, `git apply --check` in D4) runs as `… || rc=$?` with the code asserted; one `fail()` that dumps `kernel.log` tail + `wf.out` tail and `exit 1`.
- **Fixed service/authority map** (spec §4.1, not environment-configurable): `work.get→default-work-registry/work-main`, `workspace.get→default-workspace/workspace-main`, `agent.run.query→default-agent-harness/agent-runs-main`, `artifact.query→default-artifact/artifact-main`, `tool.run.query→default-tool-runner/toolruns-main`, `review.query→default-review/reviews-main`, `session.query→default-session/sessions-main`, `blob.get→default-blob/blob-main`, `artifact.get→default-artifact/artifact-main` (harness handoff only). `.query`/`.get` go through `.bin/vibe-raw -cap <name> -kind query -service <svc> -authority <auth> -payload <json>` exactly as `scripts/verify-real-provider.sh` does.
- **Commit format** — reviewer's own doc commits may stay English `docs:`; the one prescribed result commit is `[M1资格][chore][记录M1.9验收结果]` (spec §8 item 4). Author `ada <oashasu@gmail.com>`.

---

### Task 1: `console-projection.sh` — library skeleton + policy + 7-query fetch

**Files:**
- Create: `scripts/lib/console-projection.sh`
- Test: `scripts/lib/console-projection.test.sh` (new, a plain bash test script — this repo has no bats; it follows the `smoke-*.sh` style: `set -euo pipefail`, `case`-match, print `OK`/`FAIL`)

**Interfaces:**
- Consumes: `.bin/vibe-raw` (built by `scripts/build.sh`), the fixed service/authority map (Global Constraints), `CONSOLE_PROJECTION_POLICY` (path to a JSON policy file, schema in spec §4.2), `CONSOLE_PROJECTION_SNAPSHOT_DIR` (empty dir the library fills in `live` mode).
- Produces: `assert_console_projection <mode> <task_id>` where `mode` ∈ `live` | `file:DIR`. All library-private names use a `projection_` prefix (spec §4.2: "must not define a generic `fail`, `DATA`, `SOCK`, or `POLICY`"). The function `return`s (never `exit`s), never changes shell options / `cd` / `export` / traps. Task 2 adds the assertions; this task establishes fetch + load + snapshot-write.

- [ ] **Step 1: Write the failing test**

`scripts/lib/console-projection.test.sh` — builds a **real `DONE` task via the mock provider** (no codex needed), then checks the library can fetch and snapshot its seven projections:

```bash
#!/usr/bin/env bash
# Unit test for scripts/lib/console-projection.sh. Uses a mock-provider DONE task
# so it needs no codex. Run via: bash scripts/lib/console-projection.test.sh
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
source scripts/lib/kernel-harness.sh
build_bins
restart_kernel

VD=".bin/vibe -socket $SOCK -identity m1-dev -token $DEV_TOKEN"
VQ=".bin/vibe -socket $SOCK -identity local-cli -token $TOKEN"

SRC="$DATA/src"; mkdir -p "$SRC"
cp -R fixtures/sample-java-project/. "$SRC/"
printf '%s\n' 'target/' > "$SRC/.gitignore"
( cd "$SRC" && git -c init.defaultBranch=main init -q )
git -C "$SRC" add -A
git -C "$SRC" -c user.email=t@e.invalid -c user.name=t -c commit.gpgsign=false commit -q -m baseline
SRC_BASE="$(git -C "$SRC" rev-parse HEAD)"

created="$($VD task create -title "proj-lib test" \
  -goal "Use Math.addExact in Calculator.add and cover positive and negative integer overflow" \
  -repo "$SRC" -scope "src/main/java/com/example/calc/Calculator.java,src/test/java/com/example/calc/CalculatorTest.java" \
  -ac AC1="mvn -q test succeeds" \
  -ac AC2="tests separately call Calculator.add(Integer.MAX_VALUE, 1) and Calculator.add(Integer.MIN_VALUE, -1) and assert ArithmeticException for both" \
  -ac AC3="only the two scoped files are changed")"
TASK="$(printf '%s\n' "$created" | sed -n 's/^task \([^ ]*\).*/\1/p')"
WC="$(printf '%s\n' "$created" | sed -n 's/.*wc \([^ ]*\).*/\1/p')"

# drive the whole chain to DONE+SEALED with the mock provider writing a plausible file
MOCKED='class Calculator {
    private Calculator() {}
    public static int add(int a, int b) { return Math.addExact(a, b); }
}'
( $VQ workflow run "$TASK" -provider mock -base-ref "$SRC_BASE" \
    -prompt "harden add with Math.addExact" \
    -build "sh -c true" -test "sh -c true" \
    -mock-write-file "src/main/java/com/example/calc/Calculator.java" \
    -mock-write-content "$MOCKED" -review-poll-ms 200 -timeout 3m > "$DATA/wf.out" 2>&1 ) &
WF=$!
REV=""
for _ in $(seq 1 300); do
  kill -0 "$WF" 2>/dev/null || break
  j="$($VQ workflow show "$TASK" -json 2>/dev/null || true)"
  case "$j" in *'"stage":"WAITING_REVIEW"'*)
    REV="$(printf '%s\n' "$j" | python3 -c 'import json,sys; d=json.load(sys.stdin); ids=[e["payload"]["review_id"] for e in d.get("events",[]) if e.get("type")=="review.requested"]; print(ids[0] if ids else "")')"
    [ -n "$REV" ] && break ;;
  esac
  sleep 0.1
done
[ -n "$REV" ] || { echo "FAIL: no review id"; cat "$DATA/wf.out"; exit 1; }
$VD review decide "$REV" -approved -reviewer m1-dev -acceptance AC1=pass -acceptance AC2=pass -acceptance AC3=pass >/dev/null
wait "$WF" || true
for _ in $(seq 1 200); do
  case "$($VQ workflow show "$TASK" 2>/dev/null || true)" in *"stage SEALED"*) break ;; esac
  sleep 0.1
done

restart_kernel

# --- the unit under test ---
source scripts/lib/console-projection.sh
export CONSOLE_PROJECTION_SNAPSHOT_DIR="$DATA/snapshots"; mkdir -p "$CONSOLE_PROJECTION_SNAPSHOT_DIR"
POL="$DATA/policy.json"
python3 - "$SRC" > "$POL" <<'PY'
import json,sys
print(json.dumps({
 "repo": sys.argv[1],
 "title":"proj-lib test",
 "goal":"Use Math.addExact in Calculator.add and cover positive and negative integer overflow",
 "acceptance_criteria":[
  {"id":"AC1","text":"mvn -q test succeeds"},
  {"id":"AC2","text":"tests separately call Calculator.add(Integer.MAX_VALUE, 1) and Calculator.add(Integer.MIN_VALUE, -1) and assert ArithmeticException for both"},
  {"id":"AC3","text":"only the two scoped files are changed"}],
 "provider":"mock","reviewer":"m1-dev",
 "files":["src/main/java/com/example/calc/Calculator.java","src/test/java/com/example/calc/CalculatorTest.java"],
 "tools":{"build":["sh","-c","true"],"test":["sh","-c","true"]}}))
PY
export CONSOLE_PROJECTION_POLICY="$POL"

if assert_console_projection live "$TASK"; then
  # Task 1 only proves fetch+snapshot; assertions land in Task 2. For Task 1 the
  # bar is: all seven snapshot files exist and are valid JSON.
  for cap in work.get workspace.get agent.run.query artifact.query tool.run.query review.query session.query; do
    python3 -c 'import json,sys; json.load(open(sys.argv[1]))' "$CONSOLE_PROJECTION_SNAPSHOT_DIR/$cap.json" \
      || { echo "FAIL: snapshot $cap.json missing or invalid"; exit 1; }
  done
  echo "CONSOLE-PROJECTION LIB FETCH: OK"
else
  echo "FAIL: assert_console_projection live returned non-zero"; exit 1
fi
```

- [ ] **Step 2: Run — verify it fails**

Run: `bash scripts/lib/console-projection.test.sh`
Expected: FAIL — `scripts/lib/console-projection.sh: No such file or directory` (the library does not exist yet), or after the file is stubbed, `assert_console_projection: command not found` / the snapshot files are absent.

- [ ] **Step 3: Implement the library's fetch/load core**

Create `scripts/lib/console-projection.sh` per spec §4.2, this task's scope only:
- header comment; `projection_`-prefixed helpers; source-safe (no `set`, `cd`, `export`, trap; never `exit`).
- `projection_rawq <cap> <payload_json>` → runs `.bin/vibe-raw -socket "$SOCK" -identity local-cli -token "$TOKEN" -cap <cap> -kind query -service <svc> -authority <auth> -payload <payload>` using the fixed map; bounded 30 s retry **only** on transport/temporary-runtime failure (spec §4.1: once a capability answers, a semantic `NOT_FOUND`/malformed response is a hard non-zero, never a retry).
- `assert_console_projection <mode> <task_id>`:
  - `mode == live`: require `CONSOLE_PROJECTION_SNAPSHOT_DIR` set + empty; `work.get {"task_id": <task>}` → parse `work_context_id`; then the other six by `{"work_context_id": <wc>}` (`workspace.get` included); write each raw payload to `$CONSOLE_PROJECTION_SNAPSHOT_DIR/<cap>.json` before asserting; a transport/parse failure `return`s non-zero without writing a synthetic snapshot.
  - `mode == file:DIR`: read exactly the seven `<cap>.json` from `DIR`; no query.
  - load `CONSOLE_PROJECTION_POLICY`; validate its schema (all required keys, `tools` has exactly `build`+`test`, `files` a non-empty set with no `..`/absolute/empty entry, `repo` absolute).
  - (assertions are Task 2 — for now, after loading all seven + the policy, `return 0`.)

- [ ] **Step 4: Run — verify it passes**

Run: `bash scripts/lib/console-projection.test.sh`
Expected: `CONSOLE-PROJECTION LIB FETCH: OK`. Also run `bash -n scripts/lib/console-projection.sh` → exit 0.

- [ ] **Step 5: Commit**

```bash
git add scripts/lib/console-projection.sh scripts/lib/console-projection.test.sh
git commit -m "test(m1.9): console-projection library — 7-projection fetch + snapshot"
```

---

### Task 2: `console-projection.sh` — assertions 1–6 + D1–D3 致残

**Files:**
- Modify: `scripts/lib/console-projection.sh`
- Modify: `scripts/lib/console-projection.test.sh`

**Interfaces:**
- Consumes: the loaded seven projections + policy from Task 1.
- Produces: `assert_console_projection` now returns non-zero with a **specific message** when any of spec §4.2 assertions 1–6 fails; green on a well-formed DONE task. Task 4's `qualify-m1.sh` calls it once `live` and then `file:` for D1–D3.

- [ ] **Step 1: Write the failing assertions test**

Extend `console-projection.test.sh`: after the green `assert_console_projection live` call, add the three data-level 致残 checks (spec §6 D1–D3), operating on a **copy** of the snapshot dir:

```bash
SNAP="$DATA/snap-copy"
projection_expect_red() {  # $1 = description, $2 = python mutation on $SNAP
  rm -rf "$SNAP"; cp -R "$CONSOLE_PROJECTION_SNAPSHOT_DIR" "$SNAP"
  python3 - "$SNAP" <<PY
$2
PY
  if assert_console_projection "file:$SNAP" "$TASK"; then
    echo "FAIL: $1 — helper returned 0 on mutated data"; exit 1
  fi
  echo "  red OK: $1"
}
projection_expect_red "D1 summary.files=[]" '
import json,sys,os
p=os.path.join(sys.argv[1],"artifact.query.json"); d=json.load(open(p))
for a in d["artifacts"]:
    if a.get("kind")=="diff": a["summary"]["files"]=[]
json.dump(d,open(p,"w"))'
projection_expect_red "D2 AgentRun.provider=mock-but-policy-says-codex — use a codex policy here" '
import json,sys,os
p=os.path.join(sys.argv[1],"agent.run.query.json"); d=json.load(open(p))
d["agent_runs"][0]["provider"]="somethingelse"
json.dump(d,open(p,"w"))'
projection_expect_red "D3 Review.diff_artifact_id -> nonexistent" '
import json,sys,os
p=os.path.join(sys.argv[1],"review.query.json"); d=json.load(open(p))
d["reviews"][0]["diff_artifact_id"]="art-does-not-exist"
json.dump(d,open(p,"w"))'
# the unmutated snapshot must still pass
assert_console_projection "file:$CONSOLE_PROJECTION_SNAPSHOT_DIR" "$TASK" || { echo "FAIL: clean snapshot no longer green"; exit 1; }
echo "CONSOLE-PROJECTION LIB ASSERTIONS: OK"
```

(D2's message: the test's mock policy has `provider: "mock"`, so mutate the snapshot's provider to a third value `somethingelse` — assertion 2 requires `provider == policy.provider`.)

- [ ] **Step 2: Run — verify it fails**

Run: `bash scripts/lib/console-projection.test.sh`
Expected: FAIL at the first `projection_expect_red` — the Task-1 library returns 0 unconditionally, so `assert_console_projection` on the D1-mutated copy still returns 0 and the test prints `FAIL: D1 … helper returned 0 on mutated data`.

- [ ] **Step 3: Implement assertions 1–6**

Add to `assert_console_projection`, exactly per spec §4.2 assertions 1–6:
1. IDE-lens: `workspace.get` returns a Workspace with `work_context_id==wc`, `repo==policy.repo`, non-empty `path`/`base_commit`, `status=="RELEASED"`, `release_policy=="preserve"`; `artifact.query` has exactly one artifact, `kind=="diff"`, `work_context_id==wc`, `summary.files_changed==len(policy.files)`, `summary.files[]` non-empty and exactly the policy's scoped paths.
2. Agent-lens: exactly one `AgentRun`; `{id, work_context_id, workspace_path, provider, status, raw_session_ref, provider_metadata}` populated; `work_context_id==wc`, `workspace_path==workspace.path`, `provider==policy.provider`, `status=="COMPLETED"`, `frame_count>0`, `provider_metadata` keys exactly `{provider, exit_code}`, `.provider==policy.provider`, `.exit_code==0`.
3. Truth chain: the exact `work.get` task/WorkContext field checks, `tool.run.query` argv/outcome checks, `review.query` join/acceptance checks, and the `evidence_snapshot` ToolRun-id checks — all as spelled out in §4.2 assertion 3.
4. Session: exactly one `SessionRecord` with `id`/`archive_ref`/`archive_hash` non-empty, outer wc/agent_run match; `event_selection` vs `RecoveryCheckpoint.canonical_event_selection` agree on correlation/ids/hashes/count; `event_count == len(event_ids) == len(event_sha256s)` (zero permitted); `RecoveryCheckpoint` field checks incl. `task_id`/`provider`/`diff_artifact_id`/`harness_native_id` **empty**.
5. Cross-projection: the seven join into one coherent view (the unconditional ADR-002 assertion — released workspace reached via `work_context_id`).
6. No private reads: (structural — the library contains no plugin-dir path, no `git`, no `blob.get`; keep it that way).
Every failure: `echo "assert_console_projection: <specific reason>" >&2; return 1`.

- [ ] **Step 4: Run — verify it passes**

Run: `bash scripts/lib/console-projection.test.sh`
Expected: `  red OK: D1 …`, `  red OK: D2 …`, `  red OK: D3 …`, then `CONSOLE-PROJECTION LIB ASSERTIONS: OK`. `bash -n` still clean.

- [ ] **Step 5: Commit**

```bash
git add scripts/lib/console-projection.sh scripts/lib/console-projection.test.sh
git commit -m "test(m1.9): console-projection assertions 1-6 + D1-D3 data mutations"
```

---

### Task 3: `qualify-m1.sh` — preconditions, SKIP matrix, full-suite gate, setup

**Files:**
- Create: `scripts/qualify-m1.sh`
- Test: `scripts/qualify-m1.skip.test.sh` (new — exercises only the SKIP paths, which need neither codex nor a full run)

**Interfaces:**
- Consumes: `scripts/lib/kernel-harness.sh`, `scripts/lib/console-projection.sh` (Tasks 1–2), env `VIBE_REAL_PROVIDER` / `M19_QUAL_BASE` / `M19_RESULT_PATH`.
- Produces: `scripts/qualify-m1.sh` with the spec §4.1 header (`skip`/`stop_client`/`fail`/`trap`), the precondition block, the `run_gate` full-suite loop, and the `setup` block (scratch `$SRC`, `SRC_BASE`, `mkdir` the projection dirs). Tasks 4 adds the run/gates/render.

- [ ] **Step 1: Write the failing SKIP test**

`scripts/qualify-m1.skip.test.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
run() {  # $1 = description ; rest = env assignments ; expects "SKIP: ..." and exit 0
  local desc="$1"; shift
  out="$(env "$@" bash scripts/qualify-m1.sh 2>&1)"; rc=$?
  case "$rc:$out" in
    0:SKIP:*) echo "  OK: $desc" ;;
    *) echo "FAIL: $desc — rc=$rc out=$out"; exit 1 ;;
  esac
}
run "no VIBE_REAL_PROVIDER"        VIBE_REAL_PROVIDER= M19_QUAL_BASE=HEAD
run "no M19_QUAL_BASE"             VIBE_REAL_PROVIDER=codex M19_QUAL_BASE=
run "M19_QUAL_BASE not a commit"   VIBE_REAL_PROVIDER=codex M19_QUAL_BASE=zzzznotacommit
# a dirty tree must SKIP:
touch scripts/.qualify-dirty-marker
run "dirty tree"                   VIBE_REAL_PROVIDER=codex M19_QUAL_BASE=HEAD
rm -f scripts/.qualify-dirty-marker
echo "QUALIFY-M1 SKIP MATRIX: OK"
```

(If `codex`/`mvn`/`java` are absent on the machine running this test, the "no VIBE_REAL_PROVIDER" case still SKIPs first — the env-var check is ordered before the tool checks in spec §4.1, so the test is valid on a machine without codex.)

- [ ] **Step 2: Run — verify it fails**

Run: `bash scripts/qualify-m1.skip.test.sh`
Expected: FAIL — `bash scripts/qualify-m1.sh: No such file or directory`.

- [ ] **Step 3: Implement the header + preconditions + full-suite gate + setup**

Create `scripts/qualify-m1.sh` transcribing spec §4.1 lines for: the `set -euo pipefail` + `cd` + `skip`/`stop_client`/`fail`/`trap` header; the full **preconditions** block (env vars, `QUAL_BASE` resolve + ancestor check, the `for tool in bash git python3 go codex mvn java` loop, `GO_VERSION`/`CODEX_VERSION`/`MVN_VERSION`/`JAVA_VERSION` capture, the Java-8 + Maven-3.9.12 + Maven-on-Java-8 `case` checks, the clean-tree check, the `M19_RESULT_PATH` present/absent branch incl. `CANONICAL_RESULT_SHA256_BEFORE`, and the M1.9 §13-row `in-progress`/`done` check); `source scripts/lib/kernel-harness.sh`; the `run_gate` helper + the exact full-suite command list (script syntax ×2, root build, plugins/cli/kernel tests, M0.5, check-arch, smoke ×5, DONE-integrity ×3 — **the DONE-integrity loop IS G4**); the `setup` block (`export VIBE_AGENT_PROVIDERS=codex`, `RESULT_PATH` + the absolute-path/outside-repo validation for the override, `build_bins`, `restart_kernel`, `mkdir -p "$DATA/projections" "$DATA/projection-snapshots"`, the `$SRC` copy + nested-`.git` guard + `.gitignore` + `git init`/`add`/`commit` + `SRC_BASE` + the `QUAL_BASE != SRC_BASE` guard + `SRC` clean check + the policy-file write from fixed values via a `python3` JSON encoder + `export CONSOLE_PROJECTION_POLICY`). End the file here for this task with `echo "QUALIFY-M1 SETUP OK (task 3 stub)"` so `bash -n` and the SKIP test can run; Task 4 replaces that line.

- [ ] **Step 4: Run — verify it passes**

Run: `bash -n scripts/qualify-m1.sh` → exit 0. Then `bash scripts/qualify-m1.skip.test.sh` → `QUALIFY-M1 SKIP MATRIX: OK`.

- [ ] **Step 5: Commit**

```bash
git add scripts/qualify-m1.sh scripts/qualify-m1.skip.test.sh
git commit -m "feat(m1.9): qualify-m1.sh — preconditions, SKIP matrix, full-suite gate, setup"
```

---

### Task 4: `qualify-m1.sh` — WAITING_REVIEW protocol, G1–G6, D1–D5, result render

**Files:**
- Modify: `scripts/qualify-m1.sh`

**Interfaces:**
- Consumes: everything from Task 3 + `assert_console_projection` (Tasks 1–2).
- Produces: the complete `qualify-m1.sh` — after the setup block it runs spec §5 steps 1–9, then §7 G1/G2/G3/G5/G6 + §6 D1–D5, then the §4.1 "on all green" result render (atomic write + validation + G1 final whitelist + the PASSED line + the printed post-pass operator sequence). No new test file — this task's deliverable is verified by `bash -n`, a spec-walkthrough checklist, and then **Task 6's real run**.

- [ ] **Step 1: Implement the WAITING_REVIEW / client-disconnect protocol (spec §5)**

Replace the Task-3 stub with spec §5 steps 1–9 verbatim in intent:
- step 1: capture the absolute 30-min wall-clock deadline; `PROMPT=…` (the exact §5 string); `( exec ".bin/vibe" … workflow run "$TASK" -provider codex -base-ref "$SRC_BASE" -prompt "$PROMPT" -build "mvn -q -DskipTests compile" -test "mvn -q test" -timeout 30m ) >"$DATA/wf.out" 2>&1 & CLIENT_PID=$!`
- step 2: poll `vibe workflow show "$TASK"` for `stage WAITING_REVIEW`, bounded by remaining time to the step-1 deadline; `CLIENT_PID` exiting early → `fail` + dump `wf.out`.
- step 3: **auxiliary** — as m1-dev, `workspace.get {"work_context_id": "$WC"}`; require exactly one `ALLOCATED` Workspace agreeing with the WorkContext + `SRC_BASE`, non-empty `path`; save the full response to `$DATA/projections/workspace.pre-release.json`. Not a selector source.
- step 4: `REVIEW_ID` via the pinned `python3` `events[]` extractor (`review.requested` event whose payload has this `task_id`+`work_context_id`); then `review.query {"work_context_id":"$WC"}` must show exactly one review, id `RID`, status `PENDING`; write the handoff JSON (`socket,task_id,work_context_id,review_id,workspace_path` — **no token, no workspace_id**).
- step 5: client disconnect — `set +e; kill -TERM "$CLIENT_PID" …; kill_rc=$?` (non-zero → `fail "client was not alive"`), then `wait "$CLIENT_PID"; rc=$?`; `case "$rc" in 143|130) CLIENT_PID="" ;; 0) fail "client exited 0 — no disconnect" ;; *) fail "unexpected client exit $rc" ;; esac`.
- step 6: print the handoff path + the exact m1-dev `review show -json` / `artifact show -json` / `git -C <ws> diff HEAD -- <two files>` inspection commands and the exact `review decide -approved -reviewer m1-dev -acceptance AC1=pass AC2=pass AC3=pass` command; then **poll** `review.query {"work_context_id":"$WC"}` every 500 ms until exactly `RID` is `APPROVED` (bounded by the step-1 deadline; `CHANGES_REQUESTED`/`NOT_FOUND`/malformed/expiry → `fail`). **No code path calls `review.decide`.**
- step 7: independent completion — as m1-dev, poll `vibe workflow show "$TASK"` (query path `workflow.engineering.get`) to `DONE` then `SEALED`; separately poll `workspace.get {"work_context_id":"$WC"}` to `RELEASED`+`preserve`; bound `min(5 min, remaining deadline)`. Never reached DONE after a valid decide → `fail` (invariant 5).
- step 8: `restart_kernel`.
- step 9: `export CONSOLE_PROJECTION_SNAPSHOT_DIR="$DATA/projection-snapshots"; assert_console_projection live "$TASK"` — one fetch, saves the seven snapshots; on non-zero → `fail`.

- [ ] **Step 2: Implement G1/G2/G3/G5/G6 (spec §7) against the saved snapshots**

- **G1** — `python3 kernel/architecture-tests/check_boundaries.py` exit 0; `git diff --name-only "$QUAL_BASE" -- kernel` empty; no `plugins/`, `contracts/`, protected-script path in the tracked-delta+untracked union; the union is exactly the 3 impl paths (production) / those 3 + tracked `docs/M1-RESULT.md` (verification). (The final 4-path whitelist re-check is in the render step.)
- **G2** — from the policy + snapshots + one out-of-library `blob.get` on `Artifact.blob_uri`: `AgentRun.provider=="codex"`; `workspace.status=="RELEASED"`/`release_policy=="preserve"`/`base_commit==SRC_BASE`; the preserved worktree `HEAD==SRC_BASE`, non-empty `git diff HEAD`, `git ls-files --others --exclude-standard` empty, changed+untracked set == the two scoped files; `Artifact.summary.files[]` matches; byte-compare the `blob.get` patch with the worktree `git diff HEAD`; whitespace-normalized `Calculator.java` contains `Math.addExact`, `CalculatorTest.java` contains the two exact overflow calls + `ArithmeticException`. codex stdout not consulted.
- **G3** — the §4.2 assertion-3 join/cardinality checks (re-run against the snapshots; they are also inside `assert_console_projection`, so G3 largely restates that pass and additionally records the evidence block).
- **G5** — after step 8's restart, all seven projections still resolve by task/wc; resolve `raw_session_ref` / `Artifact.blob_uri` / `archive_ref` / `tracked_patch_ref` / both ToolRun stdout+stderr URIs via `blob.get` (first four non-empty; tool blobs resolve even at zero bytes); parse the archive's `session_record`/`recovery_checkpoint`/`canonical_events`, require the nested checkpoint == public checkpoint, nested identity/selection == public SessionRecord, nested `archive_ref`/`archive_hash` empty strings, each nested event `id`/`sha256` matches the selection arrays in order, `len(canonical_events)==event_count`, `sha256(archive bytes)==SessionRecord.archive_hash`.
- **G6** — `blob.get` the `tracked_patch_ref`; `git clone "$SRC"` fresh → `git checkout "$SRC_BASE"` → `git apply --check` (`|| rc=$?`, non-zero is a FAIL here) → `git apply`; resulting tracked changed set == the two scoped files, untracked empty; `RecoveryCheckpoint.{work_context_id,agent_run_id,base_commit}` + `workspace.base_commit` match.

- [ ] **Step 3: Implement the D1–D5 致残 sweep (spec §6) against a copy of the snapshots**

- D1: copy snapshot dir; `python3` set `Artifact{kind=diff}.summary.files=[]`; `assert_console_projection "file:$copy" "$TASK"` must be non-zero (`|| :` then assert the non-zero); message contains "summary.files". Discard copy, re-assert the clean snapshot green.
- D2: copy; set `AgentRun.provider="mock"`; helper non-zero, message mentions provider/policy. Restore.
- D3: copy; `Review.diff_artifact_id` → nonexistent id; helper non-zero, message mentions the diff artifact. Restore.
- D4: run the normal G6 patch-apply green first and record it; `blob.get` the patch; require a context line in the first hunk; in a temp copy replace that context line's payload with ` __M19_IMPOSSIBLE_CONTEXT__` (leading space kept); `git apply --check` against a clean `$SRC`@`$SRC_BASE` (`|| rc=$?`) must be non-zero; delete the temp copy; the normal G6 procedure green again.
- D5: no mutation — the full-suite gate's `qualify-done-integrity.sh` ×3 output (already captured in `$DATA/full-suite.log`) is the G4/D5 evidence; do not run a fourth copy.
After the sweep `git status --porcelain` must be unchanged from setup (the script only ever read live state; the mutations were on `$DATA` copies).

- [ ] **Step 4: Implement the "on all green" result render (spec §4.1)**

Render the full `docs/M1-RESULT.md` content (spec §4.4: `QUAL_BASE`, `SRC_BASE`, the real ids, the codex/Maven/Java/Go versions, the six G-gate evidence blocks, D1–D4 red/restore evidence + D5 G4 evidence, the observed canonical-event count incl. permitted zero, the invariant-11 note, and the **two verdicts**: `M1 ENGINEERING VERTICAL SLICE: PASSED` and `ADR-002 read-projection result: PASS — the cold-start task-only Console path reconstructs the full WorkContext view including the released workspace, via M1.8.5's workspace.get{work_context_id}`) into a temp file under `$DATA`; validate it contains both verdict lines and no client token / auth material / raw prompt bytes; `mv` it atomically to `$RESULT_PATH` (`fail` + no PASS on any validation/move failure). In verification mode, first require `sha256(docs/M1-RESULT.md) == CANONICAL_RESULT_SHA256_BEFORE`. Re-run the G1 final whitelist against the tracked-delta+untracked union **including** the just-written canonical `docs/M1-RESULT.md` (production) — must be exactly the four canonical paths. Then `echo "M1 ENGINEERING VERTICAL SLICE: PASSED"`, the "run-level result only" comment, and (production only) the printed post-pass operator sequence (flip the §13 marker, `git add` the four paths, `git commit -m "[M1资格][chore][记录M1.9验收结果]"`, require clean `git status --porcelain --untracked-files=all`). The script creates no commit and no tag.

- [ ] **Step 5: Verify — syntax + spec walkthrough**

Run: `bash -n scripts/qualify-m1.sh` → exit 0.
Then, with the spec open, walk §4.1 + §5 + §6 + §7 line-by-line and confirm each pinned command / payload / `case` pattern / bound is present in the script with the same text. Record any deliberate deviation. (Behavioral verification is Task 6 — a mock run cannot exercise the `-provider codex` requirement or a real Maven build.)

- [ ] **Step 6: Commit**

```bash
git add scripts/qualify-m1.sh
git commit -m "feat(m1.9): qualify-m1.sh — WAITING_REVIEW protocol, G1-G6, D1-D5, result render"
```

---

### Task 5: `docs/M1-DESIGN.md` reconciliation (the implementation commit)

**Files:**
- Modify: `docs/M1-DESIGN.md`

**Interfaces:**
- Consumes: nothing.
- Produces: the third file of the implementation delta. After this task the three impl paths (`scripts/qualify-m1.sh`, `scripts/lib/console-projection.sh`, `docs/M1-DESIGN.md`) are committed and `QUAL_BASE` for Task 6 is this commit.

- [ ] **Step 1: Edit §10 — the qualification task**

Replace the current §10 task text (`Calculator.add` "null / 溢出" — undecidable) with spec §3's decidable task: `Math.addExact`, `ArithmeticException` on overflow, the two scoped repo-relative paths, AC1 `mvn -q test`, AC2 the two exact overflow calls, AC3 `git diff + git ls-files --others` only the two files. Replace the run snippet with a pointer to §4.1/§5 (exact prompt/build/test args + the WAITING_REVIEW client-disconnect protocol). Replace `vibe review show <task-id>` with the two-step `review_id` from `workflow show -json` → `review show <review-id>`, diff via `diff_artifact_id` → `artifact.get`/`blob.get` or the worktree. Add the notes from spec §4.3 §10 bullet: codex-must-not-commit; the checkpoint carries wc/agent_run/patch-ref but not task/provider/diff-artifact (invariant 8) and its canonical-event selection may be empty; "agent runtime" = agent-harness runtime restart; the post-restart Console view is reconstructed from `task_id` alone (M1.8.5) and the ADR-002 read-projection result is **unconditional**.

- [ ] **Step 2: Edit §6/§7/§9 — the wiring reconciliation**

Per spec §4.3 first bullet: the Review snapshot's `evidence_ref_id` currently holds the ToolRun id (`AttachEvidence` returns only an error) — M1.9 checks the real WorkContext EvidenceRefs separately and does not call that snapshot field an EvidenceRef id. Workflow child calls keep the outer request correlation; the `session.seal` payload carries neither `correlation_id` nor `event_ids`, so `canonical_event_selection` can be empty — M1.9 checks selection-array/archive shape agreement, not a non-empty selection.

- [ ] **Step 3: Edit §2 — expand G1–G6**

Replace each of the §2 G1–G6 one-line criteria with the concrete assertion summary from spec §7.

- [ ] **Step 4: Edit §13 — the M1.8.5 row + the M1.9 in-progress marker**

Add, immediately before the M1.9 row:
```
M1.8.5  workspace.get{work_context_id} 找回已释放(preserve)workspace（ALLOCATED 优先，否则最新 preserve-RELEASED；确定性 tiebreak）— done (tag m1.8.5-workspace-by-context-recovery, merge 4be9939)
```
Change the M1.9 row to end with the exact `— in-progress` marker from spec §4.3 (the "implementation commit" line). Do **not** add the result suffix — that is Task 6.

- [ ] **Step 5: Verify**

Run: `python3 scripts/check-contracts.py --root contracts` → still `31 contracts` (M1-DESIGN is not a contract, but run it to confirm nothing else moved). Run:
```bash
python3 -c 'rows=[l for l in open("docs/M1-DESIGN.md",encoding="utf-8") if l.startswith("M1.9 ")]; assert len(rows)==1 and "— in-progress" in rows[0], rows'
python3 -c 'rows=[l for l in open("docs/M1-DESIGN.md",encoding="utf-8") if l.startswith("M1.8.5")]; assert len(rows)==1 and "— done" in rows[0], rows'
```
Expected: both exit 0.

- [ ] **Step 6: Commit (the implementation commit)**

```bash
git add docs/M1-DESIGN.md
git commit -m "docs(m1.9): §10 decidable task, §2 G1-G6, §6/§7/§9 wiring, §13 M1.8.5 + M1.9 in-progress"
```
Record this commit SHA — it is `M19_QUAL_BASE` for Tasks 6 and 7.

---

### Task 6: The real production run (operator, dev machine)

**Files:** produces `docs/M1-RESULT.md`; edits `docs/M1-DESIGN.md` §13 marker.

This task is **not** TDD and **not** automatable — it is the real qualification: real codex, real Maven, an interactive human review from a second terminal. Do it on the dev machine (codex-cli 0.152.x, Maven 3.9.12 on Java 8, warm `~/.m2`).

- [ ] **Step 1: Pre-flight**

`git status --porcelain --untracked-files=all` empty; `git rev-parse HEAD` == the Task-5 implementation commit; `command -v codex mvn java`; `java -version` shows 1.8; `mvn -v` shows `Apache Maven 3.9.12` on Java 1.8.

- [ ] **Step 2: Run the qualification**

```bash
M19_QUAL_BASE="$(git rev-parse HEAD)" VIBE_REAL_PROVIDER=codex bash scripts/qualify-m1.sh
```
The script runs the full regression suite (several minutes), then starts the codex workflow. It will **pause at `WAITING_REVIEW`** and print a handoff file path + exact commands.

- [ ] **Step 3: Human review from a second terminal**

In another terminal, run the printed `review show -json` / `artifact show -json` / `git -C <ws> diff HEAD -- …` commands, read the diff, confirm codex used `Math.addExact` and added the two overflow tests and only touched the two files. Then run the printed `vibe review decide … -approved -reviewer m1-dev -acceptance AC1=pass -acceptance AC2=pass -acceptance AC3=pass`. If codex produced a bad patch or failing tests, that is a **real negative result** — the gate correctly refuses DONE; re-run from Step 2 (codex is non-deterministic).

- [ ] **Step 4: Let the script finish**

The script polls to `DONE`/`SEALED`, restarts the kernel, fetches the seven projections, runs G1–G6 + D1–D5, and — on all green — writes `docs/M1-RESULT.md`, re-checks the four-path whitelist, prints `M1 ENGINEERING VERTICAL SLICE: PASSED`, then prints the post-pass operator sequence.

- [ ] **Step 5: The result commit**

Follow the printed sequence exactly: change the `docs/M1-DESIGN.md` §13 M1.9 marker from `— in-progress` to `— done` and append **only** the prescribed result suffix from spec §4.3 (the `result commit` line — ends `ADR-002 read-projection result: PASS（…M1.8.5…）`). Then:
```bash
git add scripts/qualify-m1.sh scripts/lib/console-projection.sh docs/M1-DESIGN.md docs/M1-RESULT.md
git commit -m "[M1资格][chore][记录M1.9验收结果]"
git diff --name-only "$M19_QUAL_BASE" HEAD    # must be exactly the four paths
git status --porcelain --untracked-files=all  # must be empty
```

- [ ] **Step 6: Report**

State: the codex-cli version used, the real ids (task/wc/agent_run/diff/tools/review/session), that G1–G6 and D1–D5 are green in the output, the result commit SHA, and that `docs/M1-RESULT.md` carries both verdict lines. Do **not** tag — that is Task 7.

---

### Task 7: Independent verification rerun + tag (reviewer)

**Files:** none changed (the verification result goes to a temp path outside the repo).

Reviewer ≠ producer (spec §8 item 6). A different person / session on the dev machine.

- [ ] **Step 1: Independent rerun**

```bash
M19_RESULT_PATH="$(mktemp -t m1.9-result.XXXXXX)" \
M19_QUAL_BASE=<the Task-5 implementation commit> \
VIBE_REAL_PROVIDER=codex bash scripts/qualify-m1.sh
```
This is a fresh real codex + Maven run with its own human review. It must print `M1 ENGINEERING VERTICAL SLICE: PASSED`, write the result to the temp path (not the repo), and the harness must confirm `sha256(docs/M1-RESULT.md)` is unchanged before and after.

- [ ] **Step 2: Independent 致残 re-sweep**

Do not trust the producer's D1–D5 evidence. Re-run each mutation from spec §6 against a fresh live projection snapshot (mock DONE task is acceptable for D1–D3 via `console-projection.test.sh`; D4 needs the real patch blob from Step 1's run; D5 is the `qualify-done-integrity.sh` ×3 that Step 1 already ran). Confirm each reproduces the exact expected red then restores green.

- [ ] **Step 3: Structural review**

`git diff --name-only <impl commit> HEAD` on `main` is exactly the four canonical paths + (Task 5's) the M1.9 spec/plan docs already at base; no `kernel/`, no `plugins/`, no `contracts/`. `docs/M1-RESULT.md` carries both verdict lines and no token/auth/prompt bytes. `docs/M1-DESIGN.md` §13 M1.9 row is `— done` with the exact prescribed suffix; the M1.8.5 row is present.

- [ ] **Step 4: Tag**

```bash
git tag -a m1.9-qualification -m "M1.9 — M1 Engineering Vertical Slice PASSED; ADR-002 read-projection PASS (M1.8.5)"
git push origin main --follow-tags
git rev-parse "m1.9-qualification^{commit}"   # == HEAD
```

- [ ] **Step 5: Report — M1 PASSED**

State the final tag, the merge/commit SHA, and that `docs/M1-RESULT.md` is the durable record. M1 is complete; next is M2.

---

## Self-Review

**1. Spec coverage** — §2 invariants: Global Constraints (verbatim) + enforced in Tasks 1–4. §3 task: Task 5 Step 1 + Task 6. §4.1 `qualify-m1.sh`: Tasks 3 (header/preconditions/gate/setup) + 4 (protocol/gates/sweep/render). §4.2 `console-projection.sh`: Tasks 1 (fetch) + 2 (assertions 1–6). §4.3 M1-DESIGN edits: Task 5. §4.4 M1-RESULT.md: Task 4 Step 4 (render) + Task 6 (produced). §5 protocol: Task 4 Step 1. §6 D1–D5: Task 2 (D1–D3 unit) + Task 4 Step 3 (in-harness) + Task 7 Step 2 (independent). §7 G1–G6: Task 4 Step 2 (G4 = the DONE-integrity ×3 in Task 3's gate). §8 acceptance: Tasks 6 (production) + 7 (verification). §9 NON-GOALS: nothing in the plan adds them. §11 verdict: ADMIT — the plan produces exactly the four paths.

**2. Placeholder scan** — the plan points into a spec that carries the exact pseudocode rather than re-transcribing ~600 lines; every step has a concrete command, a concrete file edit, or a concrete `python3` assertion. The one unavoidable non-TDD stretch (Task 4, a codex-and-Maven harness that a mock run can't fully exercise) is explicit about *why* and defers behavioral proof to Task 6 with a spec-walkthrough checklist in between — not a vague "test it later".

**3. Type/name consistency** — `assert_console_projection <mode> <task_id>` (2 args, no `workspace_id`) is consistent across Tasks 1, 2, 4, 7. `CONSOLE_PROJECTION_POLICY` / `CONSOLE_PROJECTION_SNAPSHOT_DIR` env names match the spec. `QUAL_BASE` (script-internal, from `M19_QUAL_BASE`) and `SRC_BASE` (scratch repo) are distinct and both required, per the spec's guardrail. The seven snapshot filenames (`<capability>.json`) and the fixed service/authority map are stated once in Global Constraints and referenced, not re-listed per task. `run_gate` (Task 3) and `fail`/`skip`/`stop_client` (Task 3 header) are used by Task 4 without redefinition.
