# M1.7 — Adversarial DONE-Integrity Qualification — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Establish evidence that the four attacks defined in `docs/M1-DESIGN.md` §4.4 are blocked: S1–S3 are exercised against a live kernel as the external identity `local-cli`; S4 is exercised at the real `runPipeline → doneGate` integration seam plus the existing gate predicate test. No claim is made that every conceivable bypass path is closed, and no new enforcement code is added.

**Architecture:** One new library script `scripts/lib/kernel-harness.sh` (kernel lifecycle, lifted verbatim out of `scripts/smoke.sh`) and one new standalone script `scripts/qualify-done-integrity.sh` that sources it, boots its own kernel against the real `config/m1-*.json`, and runs scenarios S1–S3 as `local-cli`, restarting the kernel after each stateful one and re-asserting. S4's predicate is proven by a new `runPipeline` integration test with fake capability closures (a live S4 path does not exist in M1 — the single-pass workflow binds one diff artifact id to both the review request and the gate). `scripts/smoke.sh` is refactored to source the shared library (behaviour unchanged). The harness is added as its own milestone-acceptance step; `scripts/check-arch.sh` is **not** touched.

**Tech Stack:** Bash (`set -euo pipefail`), the existing `.bin/vibe` / `.bin/vibe-kernel` / `.bin/vibe-raw` binaries, Go 1.x (`go test`) for the S4 integration test and the transient 致残对照 mutations. No new Go dependency or Python code/dependency; Task 6 only invokes the existing kernel qualification script.

**Spec:** `docs/superpowers/specs/2026-08-30-m1-7-done-integrity-qualification-design.md` (and, through it, `docs/M1-DESIGN.md` §2 G4 / §4.2 / §4.3 / §4.4 / §10 / §13).

## Global Constraints

- **Scope of the claim:** this milestone establishes evidence for the **four attacks enumerated in §4.4**: S1–S3 have live-kernel coverage, while S4 has `runPipeline → doneGate` seam coverage plus the gate predicate case. It does not — and must not be described as — proving every conceivable bypass path is closed.
- **G1 Kernel Purity:** no task modifies `kernel/` (any path). Check: `git diff --name-only "$BASE" HEAD -- kernel` must be empty (`$BASE` captured below). If a step seems to need a kernel change, stop and report.
- **No new external Go modules.**
- **No policy loosening.** `config/m1-policy.json` ships unchanged. It is edited only *transiently* inside the 致残对照 sweep and restored (`git checkout`) in the same step.
- **No test-only hook in production code.** `runPipeline` / handlers / gate get no new flags or branches for the harness. S4 rides on `pipeline_test.go` fakes, not on production code.
- **`scripts/check-arch.sh` is not modified.** It stays static and ~1 s.
- **Do NOT touch `docs/M1-DESIGN.md`.** Not staged, not edited, not committed. §13 is the reviewer's post-merge step.
- **Module paths:** kernel `github.com/example/agent-native-microkernel`; plugins `github.com/example/agent-native-os/plugins`; CLI `github.com/example/agent-native-os/cli`.
- **Assertion discipline:** every acceptance-affecting command either captures its exit status and fails closed or is part of a fail-closed conditional; output assertions capture the output and match with `case "$var" in *"..."*)` where practical. Never `cmd | grep -q` and never `grep -o ... | head -1` — under `set -o pipefail` a downstream reader closing the pipe SIGPIPEs the producer and trips the pipeline (the flake M1.5 Task 8 and M1.6 hit). Use `grep -m1 -o` when you need the first match.
- **Number discipline:** contract/manifest counts are unchanged at **31 contracts / 10 manifests**. If `check-arch.sh` prints different numbers, trust the command, note it, do not "fix" anything.
- **Quote every path that contains a variable** (`"$DATA/x"`, `"/tmp/m17-$i.log"`). Fixed literal script paths (`bash scripts/build.sh`, `source scripts/lib/kernel-harness.sh`) follow the repo's existing unquoted style.
- **Commit trailer** — every commit message ends with exactly:
  ```
  Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
  Plan: docs/superpowers/plans/2026-08-30-m1-7-done-integrity-qualification.md
  ```
  Commit summary style follows the repo's existing convention — conventional-commits in English, e.g. `test(m1.7): ...`, `refactor(smoke): ...` (see `git log`). There is no `AGENTS.md` in this repo.
- **Commit identity:** author `ada <oashasu@gmail.com>` (the connector may substitute its own — known limit, not a deviation).
- **`go build ./cli/...` may drop a `./vibe` binary in cwd** — delete it, do not commit (`.gitignore` already has `/vibe`).

**Base:** branch `chatgpt/m1-7-done-integrity-qualification` from the tarball snapshot commit. Before Task 1: `BASE=$(git rev-parse HEAD)` — use `$BASE` for every G1 check. Do not hardcode a SHA. This plan file is already committed at `$BASE`, so it will **not** appear in `git diff "$BASE" HEAD`.

---

## Background the executor needs

**The two identities** (`config/m1-policy.json`):

- `local-cli` — the **qualification / attacker** identity. Grants include `work.create@1`, `work.get@1`, `workflow.engineering.run@1`, `workflow.engineering.get@1`, `review.request@1`, `review.decide@1`, `review.get@1`, `review.query@1`, assorted `*.get@1`/`*.query@1`, `event.journal.replay@1`. **No `work.transition@1`** — direct or delegated-out. It reaches `work.transition` only *inside* the `workflow.engineering.run@1` delegation scope, i.e. only when the workflow plugin calls it on the caller's behalf.
- `m1-dev` — trusted dev identity with broad direct grants, used by the per-plugin fragment smokes. The harness uses it only for setup (`task create`) and status reads (`task show`).

**The DONE gate** (`plugins/engineering-workflow/gate.go`, `doneGate(build, test EvidenceOutcome, review ReviewState, currentDiffArtifactID string) (bool, string)`): first-failure-wins conjunction — `build.Outcome == "PASS"`, `test.Outcome == "PASS"`, `review.Status == "APPROVED"`, `review.DiffArtifactID == currentDiffArtifactID`, `len(review.AcceptanceResults) > 0`, every `result.Satisfied`. Returns e.g. `(false, "test: outcome FAIL")`, `(false, "diff: reviewed X != current Y")`. `runPipeline` calls it once, just before `work.transition(DONE)`, and on failure returns `Outcome: "GATE_FAILED", Reason: <why>` without transitioning.

**The pipeline and reviews** (`plugins/engineering-workflow/pipeline.go`, `runPipeline`): always calls `ReviewRequest` itself, stores the returned id in `res.ReviewID`, and polls **only that id** via `ReviewGet` until `Status != "PENDING"`. A pre-existing `APPROVED` review for the same WorkContext is never looked at. The `diff` artifact id is a single local variable, passed identically to `ReviewRequest` and to `doneGate` — there is no code path in M1 where they differ. On `ctx` deadline anywhere in the run, returns `Outcome: "TIMEOUT"`.

**`pipeline_test.go`** already has a `fakePipeline` struct whose `caps()` method returns a `caps` of fake closures. `CollectDiff` returns `("art-1", 1, nil)`. `ReviewGet` walks `f.reviews []ReviewState` (falling back to the last element once exhausted), or returns `approved("art-1", true)` when `f.reviews` is empty. `approved(diff string, acc ...bool) ReviewState` is a helper defined in `gate_test.go` (same `package main`). `baseRun()` returns a minimal valid `RunRequest`.

**CLI output shapes** (`cli/vibe/main.go`) — match these exact substrings:

| command | stdout | on failure |
|---|---|---|
| `vibe task create -title T -goal G -repo R -ac ID=TEXT` | `task <id>  wc <id>  status PLANNED  version 1` | — |
| `vibe task show <task-id>` | multi-line incl. `status <STATUS>` on its own line | — |
| `vibe task transition <wc-id> -to <S> -expected-version <n>` | `status <S>  version <n>` | stderr: `... host policy did not grant external identity local-cli permission for work.transition@1`, exit 1 |
| `vibe review request <wc-id> -diff-artifact <id> [-evidence k:o]...` | `review <id>  status PENDING` | — |
| `vibe review decide <rev-id> -approved -reviewer X -acceptance ID=pass` | `review <id>  status APPROVED` | — |
| `vibe workflow run <task-id> -prompt P [-build C] [-test C] [-review-poll-ms N] [-mock-write-file F] [-mock-write-content S] [-timeout D]` | line 1 `outcome <O>  task <id>  reason <R>`; line 2 `work_context <id>  agent_run <id>  diff <id>  review <id>  session <id>`; line 3 `build_tool_run <id>  test_tool_run <id>  events <n>` | returns error and **exits 1 whenever `<O> != DONE`** |
| `vibe workflow show <task-id> [-json]` | `stage <S>  outcome <O>  events <n>` then indented event types; `-json` prints the raw payload (contains `"review_id":"..."` inside the `review.requested` event) | — |

`review.query` has **no** `vibe` subcommand; call it via `.bin/vibe-raw ... -cap review.query -kind query -service default-review -authority reviews-main -payload '{"work_context_id":"<wc>"}'` (bound in `config/m1-bindings.json`; `local-cli` holds `review.query@1`). Response shape: `{"reviews":[{"id":"...","status":"...","diff_artifact_id":"...", ...}, ...]}`.

Because `vibe workflow run` exits non-zero on any non-DONE outcome, always invoke it as `... 2>&1 || true` (foreground) or check `$?` after `wait` (background) and assert on the captured text.

**Kernel restart in a script:** `restart_kernel` (from the shared library) SIGKILLs the kernel process tree, waits for the socket, then probes `event.journal.replay` + `blob.stat` until both answer. Call it directly (not in a subshell).

---

## File Structure

New:

- `scripts/lib/kernel-harness.sh` — kernel lifecycle for live-integration scripts. Sets `DATA` / `SOCK` / `VIBE_DATA_ROOT` / `TOKEN` / `DEV_TOKEN` / `VIBE_CLIENT_TOKEN` / `KPID`; defines `build_bins`, `kill_kernel_tree`, `restart_kernel`; exports them; installs the EXIT trap that kills the tree and removes `DATA`. Sourced *after* the caller has `cd`-ed to repo root. One responsibility: kernel process lifecycle + its env.
- `scripts/qualify-done-integrity.sh` — the M1.7 live qualification (S1–S3, and an S4 pointer note). Boots one kernel, defines `$VQ` (local-cli) and `$VD` (m1-dev), builds a throwaway git repo, restarts + re-asserts after each stateful scenario, prints `DONE-INTEGRITY QUALIFICATION: OK`. One responsibility: the live §4.4 attacks.

Modified:

- `scripts/smoke.sh` — replace the inline kernel-lifecycle block (current lines 5–61) with `source scripts/lib/kernel-harness.sh` + `build_bins`. No behavioural change.
- `plugins/engineering-workflow/pipeline_test.go` — one new test, `TestRunPipelineGateFailOnStaleDiff`.

Untouched: `kernel/`, `plugins/**` except that one test file (and the 致残对照 sweep's transient, reverted mutations), `config/**` except the sweep's transient mutation, `contracts/**`, `plugins/manifests/**`, `scripts/check-arch.sh`, `cli/**`, `docs/M1-DESIGN.md`.

---

## Task 1: Extract the shared kernel-harness library

**Files:**
- Create: `scripts/lib/kernel-harness.sh`
- Modify: `scripts/smoke.sh` (lines 5–61 region)

**Interfaces:**
- Produces: a sourceable file that, once sourced from repo root, provides shell vars `DATA`, `SOCK`, `TOKEN` (`m1-local-cli-token`), `DEV_TOKEN` (`m1-dev-token`), `KPID`, and functions `build_bins` (runs `bash scripts/build.sh >/dev/null`), `kill_kernel_tree <pid>`, `restart_kernel` (`exit 1` on failure). Installs `trap 'kill_kernel_tree "${KPID:-}"; rm -rf "$DATA"' EXIT`.
- Consumes: nothing from other tasks.

- [ ] **Step 1: Baseline — smoke is green before the refactor**

```bash
bash scripts/smoke.sh >/tmp/m17-smoke-base.log 2>&1 ; rc=$?
tail -3 /tmp/m17-smoke-base.log
[ "$rc" -eq 0 ] && grep -qx 'M1 SMOKE: PASSED' /tmp/m17-smoke-base.log && echo BASELINE_OK || { echo "baseline smoke rc=$rc"; exit 1; }
[ "$(pgrep -f 'vibe-kernel|plugins/manifests' | wc -l | tr -d ' ')" = 0 ] && echo NO_ORPHANS || { echo orphans; exit 1; }
```
Expected: `M1.6 WORKFLOW SMOKE: OK` / `M1 SMOKE: PASSED` in the tail, then `BASELINE_OK`, then `NO_ORPHANS`.

- [ ] **Step 2: Create `scripts/lib/kernel-harness.sh`**

Create the file with exactly this content (function bodies lifted verbatim from `scripts/smoke.sh` — do not "improve" them):

```bash
#!/usr/bin/env bash
# Kernel lifecycle for live-integration scripts (smoke, qualification).
# Source this AFTER `cd`-ing to the repo root. It sets DATA/SOCK/tokens, defines
# build_bins + kill_kernel_tree + restart_kernel, exports them, and installs an
# EXIT trap that SIGKILLs the kernel process tree and removes DATA.

DATA="$(mktemp -d)"
SOCK="$DATA/kernel.sock"
export VIBE_DATA_ROOT="$DATA/data"
TOKEN='m1-local-cli-token'
DEV_TOKEN='m1-dev-token'
export VIBE_CLIENT_TOKEN="$TOKEN"
export DEV_TOKEN
KPID=""

build_bins() { bash scripts/build.sh >/dev/null; }

# kill_kernel_tree stops the kernel AND its plugin child processes. The kernel
# spawns plugins via exec.CommandContext; SIGKILL to those children on ctx-cancel
# is async, so without an explicit tree kill each restart leaks ~7 plugin
# processes. Accumulated orphans starve the CPU and make a fresh plugin's 5s
# handshake time out.
kill_kernel_tree() {
  [ -n "${1:-}" ] || return 0
  # Kill the plugin children FIRST, while the kernel is still their parent — once
  # the kernel exits they are reparented to init and `pkill -P` can't find them.
  pkill -KILL -P "$1" 2>/dev/null || true
  for _ in $(seq 1 100); do
    pgrep -P "$1" >/dev/null 2>&1 || break
    sleep 0.02
  done
  kill -KILL "$1" 2>/dev/null || true
  wait "$1" 2>/dev/null || true
}

restart_kernel() {
  kill_kernel_tree "${KPID:-}"
  rm -f "$SOCK"
  .bin/vibe-kernel -plugins ./plugins/manifests -policy ./config/m1-policy.json \
    -bindings ./config/m1-bindings.json -contracts ./contracts -socket "$SOCK" \
    >>"$DATA/kernel.log" 2>&1 &
  KPID=$!
  for _ in $(seq 1 300); do [ -S "$SOCK" ] && break; sleep 0.03; done
  [ -S "$SOCK" ] || { echo "FAIL: kernel socket did not appear"; cat "$DATA/kernel.log"; exit 1; }
  # The socket binds before routing settles; probe a dependency-free provider
  # (event.journal) and the shared dependency (blob) until both answer.
  for _ in $(seq 1 200); do
    if .bin/vibe-raw -socket "$SOCK" -identity local-cli -token "$TOKEN" \
         -cap event.journal.replay -kind query -service default-event-journal \
         -authority journal-main -payload '{}' >/dev/null 2>&1 \
       && .bin/vibe-raw -socket "$SOCK" -identity local-cli -token "$TOKEN" \
         -cap blob.stat -kind query -service default-blob -authority blob-main \
         -payload '{"uri":"blob://sha256/0000000000000000000000000000000000000000000000000000000000000000"}' >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.05
  done
  echo "FAIL: kernel did not become query-ready after restart"; cat "$DATA/kernel.log"; exit 1
}

export SOCK DATA TOKEN KPID
export -f restart_kernel
trap 'kill_kernel_tree "${KPID:-}"; rm -rf "$DATA"' EXIT
```

Then: `chmod +x scripts/lib/kernel-harness.sh`.

- [ ] **Step 3: Rewrite the top of `scripts/smoke.sh`**

`scripts/smoke.sh` lines 1–4 (shebang, comment, `set -euo pipefail`, `cd "$(dirname "$0")/.."`) stay. Delete everything from line 5 (`bash scripts/build.sh >/dev/null`) through the `trap 'kill_kernel_tree "${KPID:-}"; rm -rf "$DATA"' EXIT` line (currently line 61), and put in its place exactly:

```bash
source scripts/lib/kernel-harness.sh
build_bins
```

The file now begins:

```bash
#!/usr/bin/env bash
# M1 smoke: real kernel + foundation/domain plugins, including restart survival.
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/lib/kernel-harness.sh
build_bins

restart_kernel

append_out="$(.bin/vibe-raw -socket "$SOCK" -identity m1-dev -token "$DEV_TOKEN" \
```

Everything from `restart_kernel` onward (the `append_out=` block, the M1.1 checks, the five `source scripts/smoke-*.sh` lines, `echo "M1 SMOKE: PASSED"`) is byte-for-byte unchanged.

- [ ] **Step 4: Smoke is still green — run it 3×**

```bash
for i in 1 2 3; do
  bash scripts/smoke.sh >"/tmp/m17-smoke-$i.log" 2>&1 ; rc=$?
  [ "$rc" -eq 0 ] || { echo "run $i FAILED rc=$rc"; tail -30 "/tmp/m17-smoke-$i.log"; exit 1; }
  grep -qx 'M1 SMOKE: PASSED' "/tmp/m17-smoke-$i.log" || { echo "run $i: no PASSED"; exit 1; }
  grep -q 'M1.6 WORKFLOW SMOKE: OK' "/tmp/m17-smoke-$i.log" || { echo "run $i: no workflow-smoke"; exit 1; }
  ! grep -q 'FAIL' "/tmp/m17-smoke-$i.log" || { echo "run $i: contains FAIL"; exit 1; }
  echo "run $i OK"
done
orph="$(pgrep -f 'vibe-kernel|plugins/manifests' | wc -l | tr -d ' ')"
[ "$orph" = 0 ] && echo NO_ORPHANS || { echo "orphans: $orph"; exit 1; }
```
Expected: `run 1 OK` … `run 3 OK`, then `NO_ORPHANS`.

- [ ] **Step 5: Commit**

```bash
rm -f ./vibe
git add scripts/lib/kernel-harness.sh scripts/smoke.sh
git commit -m "$(cat <<'EOF'
refactor(smoke): extract scripts/lib/kernel-harness.sh

Kernel lifecycle (build_bins / kill_kernel_tree / restart_kernel + env +
EXIT trap) lifted verbatim out of scripts/smoke.sh so the M1.7
qualification harness can reuse it. smoke.sh behaviour unchanged: 3/3
green, 0 orphans.

Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
Plan: docs/superpowers/plans/2026-08-30-m1-7-done-integrity-qualification.md
EOF
)"
```

---

## Task 2: Qualification skeleton + `fail()` + S1

**Files:**
- Create: `scripts/qualify-done-integrity.sh`

**Interfaces:**
- Consumes: `scripts/lib/kernel-harness.sh` (`build_bins`, `restart_kernel`, `$DATA`, `$SOCK`, `$TOKEN`, `$DEV_TOKEN`).
- Produces: an executable script exiting 0 with `DONE-INTEGRITY QUALIFICATION: OK` when every scenario holds; otherwise `fail "<msg>"` prints the message + `kernel.log` tail and exits 1. Later tasks insert S2/S3/S4 blocks *before* the final `echo`. Shell function in scope for later tasks: `fail "<msg>"`.

- [ ] **Step 1: Write the script with skeleton + `fail()` + S1**

Create `scripts/qualify-done-integrity.sh` with exactly:

```bash
#!/usr/bin/env bash
# M1.7 adversarial DONE-integrity qualification. Drives the LIVE kernel as the
# EXTERNAL identity (local-cli) and proves the four docs/M1-DESIGN.md §4.4
# attacks cannot reach DONE. Sibling of kernel/tests/integration/m05_qualification.py.
#
# Scope: proves the four §4.4 attacks are blocked. Not a proof that every
# conceivable bypass is closed.
#
# Discipline: every assertion captures output into a variable and matches with
# `case` -- never `cmd | grep -q`, never `grep -o | head -1` (SIGPIPE trips
# pipefail; the flake M1.5/M1.6 hit). `vibe workflow run` exits non-zero on any
# non-DONE outcome, so it is always run as `... 2>&1 || true` / checked via $?.
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/lib/kernel-harness.sh
build_bins
restart_kernel

fail() {  # $1 = message
  echo "$1"
  echo "--- kernel.log tail ---"
  tail -n 40 "$DATA/kernel.log" 2>/dev/null || true
  exit 1
}

VQ=".bin/vibe -socket $SOCK -identity local-cli -token $TOKEN"   # attacker / qualification identity
VD=".bin/vibe -socket $SOCK -identity m1-dev -token $DEV_TOKEN"  # trusted setup + status reads

# Throwaway source repo for the workflow's worktree.
SRC="$DATA/qsrc"; mkdir -p "$SRC"
git -C "$SRC" -c init.defaultBranch=main init -q
printf 'class Calc { int add(int a,int b){return a+b;} }\n' > "$SRC/Calc.java"
git -C "$SRC" add -A
git -C "$SRC" -c user.email=s@t -c user.name=s -c commit.gpgsign=false commit -q -m init

# ---------- S1: external identity has NO work.transition@1 (direct or delegated-out) ----------
created="$($VD task create -title "s1" -goal "g" -repo "$SRC" -ac AC1="x")"
S1_WC="$(printf '%s\n' "$created" | sed -n 's/.*wc \([^ ]*\).*/\1/p')"
[ -n "$S1_WC" ] || fail "S1 FAIL: task create: $created"
for target in IN_PROGRESS IN_REVIEW DONE FAILED; do
  out="$($VQ task transition "$S1_WC" -to "$target" -expected-version 1 2>&1 || true)"
  case "$out" in
    *"did not grant"*) : ;;
    *) fail "S1 FAIL: local-cli was NOT denied work.transition -to $target: $out" ;;
  esac
done
echo "S1 OK: external direct work.transition denied for every target state"

echo "DONE-INTEGRITY QUALIFICATION: OK"
```

Then: `chmod +x scripts/qualify-done-integrity.sh`.

- [ ] **Step 2: Run it — S1 passes**

```bash
out="$(bash scripts/qualify-done-integrity.sh 2>&1)"; rc=$?
printf '%s\n' "$out"
[ "$rc" -eq 0 ] || { echo "qualification failed rc=$rc"; exit 1; }
echo "qual exit=$rc"
```
Expected: `S1 OK: external direct work.transition denied for every target state`, then `DONE-INTEGRITY QUALIFICATION: OK`, then `qual exit=0`.
Run: `pgrep -f 'vibe-kernel|plugins/manifests' | wc -l` → `0`.

- [ ] **Step 3: Falsify S1 (致残对照) — grant `work.transition@1`, confirm red, revert**

Edit `config/m1-policy.json`: add `"work.transition@1",` as the first element of `grants."local-cli".capabilities`.

```bash
out="$(bash scripts/qualify-done-integrity.sh 2>&1)"; rc=$?
printf '%s\n' "$out"
[ "$rc" -ne 0 ] || { echo "expected S1 qualification failure"; exit 1; }
case "$out" in *"S1 FAIL: local-cli was NOT denied work.transition -to IN_PROGRESS"*) echo "M-S1 RED" ;; *) echo "unexpected S1 mutation output"; exit 1 ;; esac
```
Expected: **FAIL** with `rc=1` — the very first loop iteration (`-to IN_PROGRESS`) now succeeds, so its `case` has no `did not grant` and the script calls `fail "S1 FAIL: local-cli was NOT denied work.transition -to IN_PROGRESS: status IN_PROGRESS  version 2"`. (Later iterations never run.)

Revert: `git checkout "config/m1-policy.json"`. Re-run → `DONE-INTEGRITY QUALIFICATION: OK`.

- [ ] **Step 4: Commit**

```bash
rm -f ./vibe
git add scripts/qualify-done-integrity.sh
git commit -m "$(cat <<'EOF'
test(m1.7): qualification harness skeleton + fail() + S1

scripts/qualify-done-integrity.sh boots its own kernel via the shared
harness lib and, as the external identity local-cli, asserts
work.transition is denied for every target state. fail() dumps the
kernel log tail. Falsified: granting local-cli work.transition@1 turns
S1 red on the first transition.

Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
Plan: docs/superpowers/plans/2026-08-30-m1-7-done-integrity-qualification.md
EOF
)"
```

---

## Task 3: S2 — failed build / failed test cannot pass the gate

**Files:**
- Modify: `scripts/qualify-done-integrity.sh` (insert an S2 block before the final `echo "DONE-INTEGRITY QUALIFICATION: OK"`)

**Interfaces:**
- Consumes: `$VQ`, `$VD`, `$SRC`, `restart_kernel`, `fail`.
- Produces: an `s2_run` shell function + two calls; prints `S2 OK: ...`.

- [ ] **Step 1: Insert the S2 block**

Immediately before the final `echo "DONE-INTEGRITY QUALIFICATION: OK"` line, insert:

```bash
# ---------- S2: failed evidence cannot pass the DONE gate ----------
# Run the workflow with a failing build or test, approve the review anyway
# (a colluding human), assert the gate still refuses DONE.
s2_run() {  # $1=build cmd  $2=test cmd  $3=expected reason substring
  local created task show j pid rev decide wf_out ts
  created="$($VD task create -title "s2" -goal "harden add" -repo "$SRC" -ac AC1="build+test pass")"
  task="$(printf '%s\n' "$created" | sed -n 's/^task \([^ ]*\).*/\1/p')"
  [ -n "$task" ] || fail "S2 FAIL: task create: $created"
  ( $VQ workflow run "$task" -prompt "harden" -build "$1" -test "$2" \
      -review-poll-ms 200 -mock-write-file Calc.java -mock-write-content '// s2
' -timeout 3m > "$DATA/s2.out" 2>&1 ) &
  pid=$!
  rev=""
  for _ in $(seq 1 600); do
    kill -0 "$pid" 2>/dev/null || break
    show="$($VQ workflow show "$task" 2>/dev/null || true)"
    case "$show" in *"stage WAITING_REVIEW"*)
      j="$($VQ workflow show "$task" -json 2>/dev/null || true)"
      rev="$(printf '%s\n' "$j" | grep -m1 -o '"review_id":"[^"]*"' | sed 's/.*:"//;s/"//')"
      [ -n "$rev" ] && break ;;
    esac
    sleep 0.1
  done
  [ -n "$rev" ] || { cat "$DATA/s2.out"; fail "S2 FAIL: never reached WAITING_REVIEW ($1 / $2)"; }
  decide="$($VQ review decide "$rev" -approved -reviewer mallory -acceptance AC1=pass 2>&1 || true)"
  case "$decide" in *"status APPROVED"*) : ;; *) fail "S2 FAIL: review decide: $decide" ;; esac
  if wait "$pid"; then cat "$DATA/s2.out"; fail "S2 FAIL: workflow run exited 0 with $1 / $2"; fi
  wf_out="$(cat "$DATA/s2.out")"
  case "$wf_out" in *"outcome GATE_FAILED"*) : ;; *) fail "S2 FAIL: outcome not GATE_FAILED ($1 / $2): $wf_out" ;; esac
  case "$wf_out" in *"$3"*) : ;; *) fail "S2 FAIL: reason lacks '$3': $wf_out" ;; esac
  ts="$($VD task show "$task" 2>&1 || true)"
  case "$ts" in *"status DONE"*) fail "S2 FAIL: task went DONE despite $1 / $2: $ts" ;; esac
  restart_kernel
  ts="$($VD task show "$task" 2>&1 || true)"
  case "$ts" in *"status DONE"*) fail "S2 FAIL: task DONE after restart: $ts" ;; esac
}
s2_run "sh -c true"  "sh -c false" "reason test:"
s2_run "sh -c false" "sh -c true"  "reason build:"
echo "S2 OK: failing test and failing build both blocked at the gate"
```

- [ ] **Step 2: Run it — S1 + S2 pass**

```bash
out="$(bash scripts/qualify-done-integrity.sh 2>&1)"; rc=$?
printf '%s\n' "$out"
[ "$rc" -eq 0 ] || { echo "qualification failed rc=$rc"; exit 1; }
echo "qual exit=$rc"
```
Expected: `S1 OK ...`, `S2 OK: failing test and failing build both blocked at the gate`, `DONE-INTEGRITY QUALIFICATION: OK`, `qual exit=0`.

- [ ] **Step 3: Falsify S2 (致残对照) — remove the test-PASS check, confirm red, revert**

In `plugins/engineering-workflow/gate.go`, delete these three lines from `doneGate`:

```go
	if test.Outcome != "PASS" {
		return false, fmt.Sprintf("test: outcome %s", test.Outcome)
	}
```

```bash
out="$(bash scripts/qualify-done-integrity.sh 2>&1)"; rc=$?
printf '%s\n' "$out"
[ "$rc" -ne 0 ] || { echo "expected S2 qualification failure"; exit 1; }
case "$out" in *"S2 FAIL: workflow run exited 0 with sh -c true / sh -c false"*) echo "M-S2 RED" ;; *) echo "unexpected S2 mutation output"; exit 1 ;; esac
```
Expected: **FAIL** with `rc=1` — the first `s2_run` (`sh -c true` / `sh -c false`) now yields `outcome DONE`, `vibe workflow run` exits 0, so `fail "S2 FAIL: workflow run exited 0 with sh -c true / sh -c false"`.

Revert: `git checkout "plugins/engineering-workflow/gate.go"`. Re-run → `S2 OK` again.

- [ ] **Step 4: Commit**

```bash
rm -f ./vibe
git add scripts/qualify-done-integrity.sh
git commit -m "$(cat <<'EOF'
test(m1.7): S2 — failed build/test cannot pass the DONE gate

Workflow run with a failing test (then a failing build), review approved
anyway; assert outcome GATE_FAILED with the right reason, task not DONE,
still not DONE after a kernel restart. Falsified: deleting the test-PASS
check in gate.go lets the failing-test run reach DONE.

Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
Plan: docs/superpowers/plans/2026-08-30-m1-7-done-integrity-qualification.md
EOF
)"
```

---

## Task 4: S3 — injected/stale review never consulted

**Files:**
- Modify: `scripts/qualify-done-integrity.sh` (insert an S3 block before the final `echo`)

**Interfaces:**
- Consumes: `$VQ`, `$VD`, `$SRC`, `$SOCK`, `$TOKEN`, `restart_kernel`, `fail`.
- Produces: prints `S3 OK: ...`.

- [ ] **Step 1: Insert the S3 block**

Immediately before the final `echo "DONE-INTEGRITY QUALIFICATION: OK"` line, insert:

```bash
# ---------- S3: an injected APPROVED review does not move the Task ----------
# local-cli holds review.request@1 + review.decide@1 directly, so it can
# fabricate an APPROVED review for the WorkContext BEFORE the workflow runs.
# The pipeline must ignore it: it polls only the review id IT created.
created="$($VD task create -title "s3" -goal "harden add" -repo "$SRC" -ac AC1="build+test pass")"
S3_TASK="$(printf '%s\n' "$created" | sed -n 's/^task \([^ ]*\).*/\1/p')"
S3_WC="$(printf '%s\n' "$created" | sed -n 's/.*wc \([^ ]*\).*/\1/p')"
{ [ -n "$S3_TASK" ] && [ -n "$S3_WC" ]; } || fail "S3 FAIL: task create: $created"

fake_out="$($VQ review request "$S3_WC" -diff-artifact art-injected-not-real \
             -evidence build:PASS -evidence test:PASS 2>&1 || true)"
RFAKE="$(printf '%s\n' "$fake_out" | sed -n 's/^review \([^ ]*\).*/\1/p')"
[ -n "$RFAKE" ] || fail "S3 FAIL: could not create injected review: $fake_out"
fdec="$($VQ review decide "$RFAKE" -approved -reviewer mallory -acceptance AC1=pass 2>&1 || true)"
case "$fdec" in *"status APPROVED"*) : ;; *) fail "S3 FAIL: could not approve injected review: $fdec" ;; esac

# Real workflow; real review left undecided; short deadline so the poll loop times out.
s3_out="$($VQ workflow run "$S3_TASK" -prompt "harden" -build "sh -c true" -test "sh -c true" \
           -review-poll-ms 200 -mock-write-file Calc.java -mock-write-content '// s3
' -timeout 20s 2>&1 || true)"
case "$s3_out" in *"outcome TIMEOUT"*) : ;; *) fail "S3 FAIL: expected TIMEOUT, got: $s3_out" ;; esac
RREAL="$(printf '%s\n' "$s3_out" | sed -n 's/.*  review \([^ ]*\)  session.*/\1/p')"
{ [ -n "$RREAL" ] && [ "$RREAL" != "$RFAKE" ]; } || fail "S3 FAIL: workflow did not open its own review (real='$RREAL' fake='$RFAKE'): $s3_out"

# Both reviews exist under the WC: assert exactly two records and bind each id to
# its expected status, rather than merely finding the two status strings somewhere
# in the payload. The review handler emits compact JSON with one object separator.
rq="$(.bin/vibe-raw -socket "$SOCK" -identity local-cli -token "$TOKEN" \
        -cap review.query -kind query -service default-review -authority reviews-main \
        -payload "{\"work_context_id\":\"$S3_WC\"}" 2>&1 || true)"
rows="$(printf '%s\n' "$rq" | sed 's/},{/}\\
{/g')"
n="$(printf '%s\n' "$rows" | grep -c '"id":"' || true)"
[ "$n" = 2 ] || fail "S3 FAIL: review.query expected exactly 2 reviews, got $n: $rq"
fake_row="$(printf '%s\n' "$rows" | grep -F "\"id\":\"$RFAKE\"" || true)"
real_row="$(printf '%s\n' "$rows" | grep -F "\"id\":\"$RREAL\"" || true)"
case "$fake_row" in *"\"id\":\"$RFAKE\""*'"status":"APPROVED"'*) : ;; *) fail "S3 FAIL: review.query injected review is not APPROVED: $rq" ;; esac
case "$real_row" in *"\"id\":\"$RREAL\""*'"status":"PENDING"'*) : ;; *) fail "S3 FAIL: review.query workflow review is not PENDING: $rq" ;; esac

ts="$($VD task show "$S3_TASK" 2>&1 || true)"
case "$ts" in *"status DONE"*) fail "S3 FAIL: task DONE despite undecided real review: $ts" ;; esac
restart_kernel
ts="$($VD task show "$S3_TASK" 2>&1 || true)"
case "$ts" in *"status DONE"*) fail "S3 FAIL: task DONE after restart: $ts" ;; esac
echo "S3 OK: injected APPROVED review is never consulted; undecided real review => TIMEOUT, not DONE"
```

- [ ] **Step 2: Run it — S1–S3 pass**

```bash
out="$(bash scripts/qualify-done-integrity.sh 2>&1)"; rc=$?
printf '%s\n' "$out"
[ "$rc" -eq 0 ] || { echo "qualification failed rc=$rc"; exit 1; }
echo "qual exit=$rc"
```
Expected: `S1 OK`, `S2 OK`, `S3 OK: injected APPROVED review is never consulted...`, `DONE-INTEGRITY QUALIFICATION: OK`, `qual exit=0`.

If `S3 FAIL: workflow did not open its own review (real='' ...)` appears, the 20 s deadline fired before the pipeline created its review on this machine — raise the `-timeout 20s` in this S3 block to `40s` and re-run. Log this as a degradation; it is not a stop.

- [ ] **Step 3: Falsify S3 (致残对照) — make review.get fall back to the WC's latest decision, confirm red, revert**

In `plugins/review/handlers.go`, in `getHandler`, replace:

```go
		r, ok := s.GetByID(q.ReviewID)
		if !ok {
			return nil, &protocol.Error{Code: "NOT_FOUND", Message: ErrNotFound.Error()}
		}
		return map[string]any{"review": r}, nil
```

with:

```go
		r, ok := s.GetByID(q.ReviewID)
		if !ok {
			return nil, &protocol.Error{Code: "NOT_FOUND", Message: ErrNotFound.Error()}
		}
		if r.Status == "PENDING" {
			for _, cand := range s.QueryByContext(r.WorkContextID) {
				if cand.Status != "PENDING" {
					r = cand
				}
			}
		}
		return map[string]any{"review": r}, nil
```

```bash
out="$(bash scripts/qualify-done-integrity.sh 2>&1)"; rc=$?
printf '%s\n' "$out"
[ "$rc" -ne 0 ] || { echo "expected S3 qualification failure"; exit 1; }
case "$out" in *"S3 FAIL: expected TIMEOUT, got:"*"outcome GATE_FAILED"*) echo "M-S3 RED" ;; *) echo "unexpected S3 mutation output"; exit 1 ;; esac
```
Expected: **FAIL** with `rc=1` — the pipeline's poll of its own (PENDING) review now resolves to `R_fake` (APPROVED); the poll loop breaks and `doneGate` rejects on the diff mismatch, so the outcome is `GATE_FAILED` not `TIMEOUT`: `fail "S3 FAIL: expected TIMEOUT, got: ... outcome GATE_FAILED ... reason diff: ..."`.
(Defence in depth this exposes: even with the poll fooled, the `diff_artifact_id` binding still blocks `DONE` — the task never actually reaches `DONE`, only the outcome label changes. S3 asserts on `outcome == TIMEOUT` precisely so it still falsifies.)

Revert: `git checkout "plugins/review/handlers.go"`. Re-run → `S3 OK` again.

- [ ] **Step 4: Commit**

```bash
rm -f ./vibe
git add scripts/qualify-done-integrity.sh
git commit -m "$(cat <<'EOF'
test(m1.7): S3 — injected/stale review is never consulted

local-cli fabricates an APPROVED review for the WorkContext before the
workflow runs; the pipeline ignores it and, with its own review left
undecided, times out rather than reaching DONE — verified via review.query
(both reviews present, one APPROVED one PENDING) and across a kernel
restart. Falsified: making review.get fall back to the WC's latest
decision turns S3 red.

Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
Plan: docs/superpowers/plans/2026-08-30-m1-7-done-integrity-qualification.md
EOF
)"
```

---

## Task 5: S4 — wrong-diff review is rejected by the gate (pipeline integration test)

**Files:**
- Modify: `plugins/engineering-workflow/pipeline_test.go` (add one test)
- Modify: `scripts/qualify-done-integrity.sh` (add the S4 pointer note before the final `echo`)

**Interfaces:**
- Consumes: `fakePipeline` / `.caps()` / `baseRun()` from `pipeline_test.go`; `approved(diff string, acc ...bool) ReviewState` from `gate_test.go`; `runPipeline(ctx, caps, RunRequest) RunResult` from `pipeline.go`.
- Produces: `TestRunPipelineGateFailOnStaleDiff` in `pipeline_test.go`; an `S4 SEAM OK` pointer line in the harness.

**Why this shape:** a genuinely live S4 (the workflow's *own* review carrying a stale diff) has no code path in M1 — `runPipeline` passes one `diff` variable to both `ReviewRequest` and `doneGate`. So S4 is proven at the `runPipeline` integration seam with fake capability closures: an APPROVED review whose `DiffArtifactID` differs from the collected diff (`"art-1"`) must make `runPipeline` return `GATE_FAILED` and skip the `DONE` transition. This is strictly more than `gate_test.go` (which unit-tests `doneGate` alone).

- [ ] **Step 1: Write the characterization test**

This locks in enforcement that already exists, so it is green on first run; Step 3 (inverting the production check and watching it go red) is what proves it bites. Add to `plugins/engineering-workflow/pipeline_test.go` (after `TestRunPipelineGateFailOnTest`):

```go
func TestRunPipelineGateFailOnStaleDiff(t *testing.T) {
	// CollectDiff (fake) returns "art-1"; the only review is APPROVED but bound
	// to a different diff artifact. runPipeline must refuse DONE.
	f := &fakePipeline{reviews: []ReviewState{approved("art-STALE", true)}}
	r := runPipeline(context.Background(), f.caps(), baseRun())
	if r.Outcome != "GATE_FAILED" || !strings.Contains(r.Reason, "diff") {
		t.Fatalf("outcome=%s reason=%s", r.Outcome, r.Reason)
	}
	if f.has("transition:DONE") {
		t.Fatal("DONE transition happened despite stale-diff review")
	}
	if !f.has("seal") || !f.has("release") {
		t.Fatal("cleanup missing")
	}
}
```

- [ ] **Step 2: Run it — green on first run**

Run: `go test ./plugins/engineering-workflow/ -run TestRunPipelineGateFailOnStaleDiff -v`
Expected: **PASS** — the behaviour already exists (`doneGate` has the `review.DiffArtifactID != currentDiffArtifactID` check). Step 3 confirms the test is exercising that path.

- [ ] **Step 3: Confirm the test bites — invert the diff check, watch it fail, restore**

In `plugins/engineering-workflow/gate.go`, delete from `doneGate`:

```go
	if review.DiffArtifactID != currentDiffArtifactID {
		return false, fmt.Sprintf("diff: reviewed %s != current %s", review.DiffArtifactID, currentDiffArtifactID)
	}
```

Run: `go test ./plugins/engineering-workflow/ -run 'TestRunPipelineGateFailOnStaleDiff|TestDoneGateFailsOnEachCondition' -v`
Expected: **both FAIL** — `TestRunPipelineGateFailOnStaleDiff` now gets `Outcome == "DONE"`; `TestDoneGateFailsOnEachCondition` fails its `wrong diff` case.

Restore: `git checkout "plugins/engineering-workflow/gate.go"`. Re-run → both PASS.

- [ ] **Step 4: Add the S4 note to the harness**

In `scripts/qualify-done-integrity.sh`, immediately before the final `echo "DONE-INTEGRITY QUALIFICATION: OK"` line, insert:

```bash
# ---------- S4: wrong-diff approval ----------
# A live S4 (the workflow's OWN review carrying a stale diff) has no code path in
# M1: runPipeline passes one diff artifact id to both review.request and the gate.
# Proven instead at the runPipeline integration seam:
#   plugins/engineering-workflow/pipeline_test.go::TestRunPipelineGateFailOnStaleDiff
#   (APPROVED review, DiffArtifactID != collected diff => GATE_FAILED, no DONE)
# plus the predicate unit test gate_test.go::TestDoneGateFailsOnEachCondition/'wrong diff'.
echo "S4 SEAM OK: wrong-diff review rejected by the gate (pipeline_test.go + gate_test.go)"
```

- [ ] **Step 5: Run the whole harness + the package tests**

```bash
if go test ./plugins/engineering-workflow/; then
  echo PKG_OK
else
  echo "engineering-workflow package tests failed"
  exit 1
fi
bash scripts/qualify-done-integrity.sh >/tmp/m17-qual-s4.log 2>&1 ; rc=$?
tail -6 /tmp/m17-qual-s4.log
last="$(tail -1 /tmp/m17-qual-s4.log)"
{ [ "$rc" -eq 0 ] && [ "$last" = "DONE-INTEGRITY QUALIFICATION: OK" ]; } && echo QUAL_OK || { echo "qual rc=$rc last='$last'"; exit 1; }
```
Expected: `PKG_OK`, then the harness tail showing `S1 OK`…`S3 OK`, `S4 SEAM OK`, and `DONE-INTEGRITY QUALIFICATION: OK`, then `QUAL_OK`.

- [ ] **Step 6: Commit**

```bash
rm -f ./vibe
git add plugins/engineering-workflow/pipeline_test.go scripts/qualify-done-integrity.sh
git commit -m "$(cat <<'EOF'
test(m1.7): S4 — wrong-diff review rejected by the gate

TestRunPipelineGateFailOnStaleDiff drives runPipeline with an APPROVED
review whose diff artifact id != the collected diff; asserts GATE_FAILED
and no DONE transition. A live S4 has no code path in M1 (one diff var
feeds both review.request and the gate) -- documented in the harness.
Falsified: deleting the diff check in gate.go turns this test and the
gate_test.go wrong-diff case red.

Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
Plan: docs/superpowers/plans/2026-08-30-m1-7-done-integrity-qualification.md
EOF
)"
```

---

## Task 6: Full acceptance + 致残对照 sweep

**Files:** none changed (verification only; re-verifies Tasks 1–5).

**Interfaces:**
- Consumes: everything built in Tasks 1–5.
- Produces: raw command output for the PR body; nothing committed unless a stray `./vibe` needs removing.

Run every step below as a fail-closed one-liner: it prints an `... OK` marker on success and
`exit 1`s on any deviation (never relies on eyeballing piped output). Paste the full
transcript into the PR.

- [ ] **Step 1: Three-module build**

```bash
go build ./plugins/... ./cli/... && ( cd kernel && go build ./... ) && echo BUILD_OK
```
Expected: `BUILD_OK`, exit 0.

- [ ] **Step 2: Go tests**

```bash
if go test ./plugins/... ./plugins/_template ./cli/... && ( cd kernel && go test ./... ); then
  echo GO_TESTS_OK
else
  echo "Go test suite failed"
  exit 1
fi
out="$(go test ./plugins/engineering-workflow/ -run TestRunPipelineGateFailOnStaleDiff -v 2>&1)"; rc=$?
[ "$rc" -eq 0 ] || { printf '%s\n' "$out"; echo "S4 test command failed rc=$rc"; exit 1; }
case "$out" in *"--- PASS: TestRunPipelineGateFailOnStaleDiff"*) echo S4_TEST_PRESENT ;; *) printf '%s\n' "$out"; echo "S4 test not passing"; exit 1 ;; esac
```
Expected: `GO_TESTS_OK`, then `S4_TEST_PRESENT`.

- [ ] **Step 3: Kernel regression untouched**

```bash
( cd kernel && ./scripts/build.sh >/dev/null && python3 tests/integration/m05_qualification.py ) ; rc=$?
[ "$rc" -eq 0 ] && echo M05_OK || { echo "M05 FAILED rc=$rc"; exit 1; }
```
Expected: the script's own `M0.5 ADVERSARIAL QUALIFICATION: PASSED` line, then `M05_OK`.

- [ ] **Step 4: Architecture checks — unchanged, still static**

```bash
out="$(bash scripts/check-arch.sh)" ; rc=$?
printf '%s\n' "$out"
[ "$rc" -eq 0 ] || { echo "check-arch exit $rc"; exit 1; }
case "$out" in *"CONTRACT CHECK: PASSED"*"COMPOSITION FITNESS: PASSED"*"ARCHITECTURE FITNESS: PASSED"*"ARCH CHECKS OK"*) echo ARCH_OK ;; *) echo "check-arch output unexpected"; exit 1 ;; esac
[ -z "$(git diff --name-only "$BASE" HEAD -- scripts/check-arch.sh)" ] && echo CHECK_ARCH_UNTOUCHED || { echo "check-arch.sh was modified"; exit 1; }
```
Expected: the four `check-arch` lines, then `ARCH_OK`, then `CHECK_ARCH_UNTOUCHED`. (If the printed contract/manifest counts differ from 31/10, trust the command and note it — M1.7 adds none.)

- [ ] **Step 5: DONE-integrity qualification — run it 3×**

```bash
for i in 1 2 3; do
  bash scripts/qualify-done-integrity.sh >"/tmp/m17-qual-$i.log" 2>&1 ; rc=$?
  [ "$rc" -eq 0 ] || { echo "qual run $i FAILED rc=$rc"; cat "/tmp/m17-qual-$i.log"; exit 1; }
  last="$(tail -1 "/tmp/m17-qual-$i.log")"
  [ "$last" = "DONE-INTEGRITY QUALIFICATION: OK" ] || { echo "qual run $i: bad last line '$last'"; cat "/tmp/m17-qual-$i.log"; exit 1; }
  echo "qual run $i OK"
done
markers="$(grep -hE '^(S[1-3] OK|S4 SEAM OK)' /tmp/m17-qual-1.log || true)"
case "$markers" in
  *"S1 OK"*"S2 OK"*"S3 OK"*"S4 SEAM OK"*) echo "S1-S3 live + S4 seam all OK" ;;
  *) printf '%s\n' "$markers"; echo "qualification markers incomplete"; exit 1 ;;
esac
orph="$(pgrep -f 'vibe-kernel|plugins/manifests' | wc -l | tr -d ' ')"
[ "$orph" = 0 ] && echo NO_ORPHANS || { echo "orphan procs: $orph"; exit 1; }
```
Expected: `qual run 1 OK` … `qual run 3 OK`, `S1-S3 live + S4 seam all OK`, `NO_ORPHANS`.

- [ ] **Step 6: Smoke still green — run it 5×**

```bash
for i in 1 2 3 4 5; do
  bash scripts/smoke.sh >"/tmp/m17-smoke5-$i.log" 2>&1 ; rc=$?
  [ "$rc" -eq 0 ] || { echo "smoke run $i FAILED rc=$rc"; tail -30 "/tmp/m17-smoke5-$i.log"; exit 1; }
  grep -qx 'M1 SMOKE: PASSED' "/tmp/m17-smoke5-$i.log" || { echo "smoke run $i: no PASSED line"; exit 1; }
  grep -q 'M1.6 WORKFLOW SMOKE: OK' "/tmp/m17-smoke5-$i.log" || { echo "smoke run $i: no workflow-smoke line"; exit 1; }
  ! grep -q 'FAIL' "/tmp/m17-smoke5-$i.log" || { echo "smoke run $i: contains FAIL"; grep 'FAIL' "/tmp/m17-smoke5-$i.log"; exit 1; }
  echo "smoke run $i OK"
done
orph="$(pgrep -f 'vibe-kernel|plugins/manifests' | wc -l | tr -d ' ')"
[ "$orph" = 0 ] && echo NO_ORPHANS || { echo "orphan procs: $orph"; exit 1; }
```
Expected: `smoke run 1 OK` … `smoke run 5 OK`, then `NO_ORPHANS`.

- [ ] **Step 7: The full 致残对照 sweep — each mutation red, then reverted**

Do each in turn: apply, run the command, confirm the red, `git checkout` the file, confirm green.

| # | Mutation (file) | Command that must go red | Expected red |
|---|---|---|---|
| M-S1 | add `"work.transition@1"` to `grants."local-cli".capabilities` — `config/m1-policy.json` | `bash scripts/qualify-done-integrity.sh` | `S1 FAIL: local-cli was NOT denied ... -to IN_PROGRESS: status IN_PROGRESS  version 2`, exit 1 |
| M-S2 | delete the `test.Outcome != "PASS"` block from `doneGate` — `plugins/engineering-workflow/gate.go` | `bash scripts/qualify-done-integrity.sh` | `S2 FAIL: workflow run exited 0 with sh -c true / sh -c false`, exit 1 |
| M-S3 | the `getHandler` fallback patch from Task 4 Step 3 — `plugins/review/handlers.go` | `bash scripts/qualify-done-integrity.sh` | `S3 FAIL: expected TIMEOUT, got: ... outcome GATE_FAILED`, exit 1 |
| M-S4 | delete the `review.DiffArtifactID != currentDiffArtifactID` block from `doneGate` — `plugins/engineering-workflow/gate.go` | `go test ./plugins/engineering-workflow/ -run 'TestRunPipelineGateFailOnStaleDiff|TestDoneGateFailsOnEachCondition'` | both FAIL |

After the sweep: `git status --porcelain` → empty.

- [ ] **Step 8: G1 anchors + change scope**

Run:
```bash
git diff --name-only "$BASE" HEAD -- kernel
git diff --name-only "$BASE" HEAD -- docs/M1-DESIGN.md
git diff --name-only "$BASE" HEAD
```
Expected: first two empty. The third lists exactly:
```
plugins/engineering-workflow/pipeline_test.go
scripts/lib/kernel-harness.sh
scripts/qualify-done-integrity.sh
scripts/smoke.sh
```
(The plan file is already at `$BASE` and does not appear.)

- [ ] **Step 9: Open the PR**

Branch `chatgpt/m1-7-done-integrity-qualification` → `main`, title **M1.7 — Adversarial DONE-Integrity Qualification**. Body:
- the 5-commit table (Task 1–5);
- the raw output of Steps 1–8;
- the 致残对照 sweep results — for each of M-S1…M-S4: the mutation, the command, the exact red line, and "green again after `git checkout`";
- a one-line scope statement: *S1–S3 are live-kernel checks and S4 is a `runPipeline → doneGate` seam plus predicate check; together they cover the four §4.4 attacks, but this is not a proof that every bypass path is closed.*
- "The reviewer will re-run all of the above independently and redo the 致残对照 — self-report is not acceptance."

---

## Self-Review

**1. Spec coverage**

| Spec item | Task |
|---|---|
| §3 In: `scripts/lib/kernel-harness.sh` extracted from smoke.sh, smoke unchanged | Task 1 |
| §3 In: `scripts/qualify-done-integrity.sh` standalone, own kernel, S1–S3 live, restart+re-assert, `DONE-INTEGRITY QUALIFICATION: OK` | Tasks 2–4 |
| §3 In: milestone acceptance gains a step; check-arch.sh untouched | Task 6 Steps 4–5; Global Constraints |
| §3 Out: no kernel / no new Go dep / no policy loosening / no production test hook / no check-arch.sh change | Global Constraints; Task 6 Step 4 & Step 8 |
| §4 S1 (four target states, `did not grant`) | Task 2 Step 1 |
| §4 S2 (failing test + failing build, approve anyway, `GATE_FAILED` + reason, not DONE, restart) | Task 3 Step 1 |
| §4 S3 (inject APPROVED review, real review undecided, `TIMEOUT`, own review id ≠ fake, `review.query` shows both, not DONE, restart) | Task 4 Step 1 |
| §4 S4 (predicate → gate_test.go; **pipeline seam** → new `TestRunPipelineGateFailOnStaleDiff`; live path documented as non-existent in M1; **no** production flag) | Task 5 |
| §5 robustness: EXIT trap, bounded polling not fixed sleeps, `-timeout` is the ctx deadline, raise-don't-sleep degradation, unified `fail()` with kernel-log tail | Task 1 Step 2 (trap); Task 2 Step 1 (`fail`); Task 3/4 Step 1 (bounded loops, `grep -m1`); Task 4 Step 2 (degradation note) |
| §6 致残对照: M-S1 policy, M-S2 gate test-check, M-S3 review getHandler, M-S4 gate diff-check (pipeline + unit) | Task 2 Step 3; Task 3 Step 3; Task 4 Step 3; Task 5 Step 3; consolidated Task 6 Step 7 |
| §7 acceptance step order (build / go test / m05 / check-arch / qualify×3 / smoke×5 / G1) | Task 6 Steps 1–8 |
| §8 observation (empty diff → DONE) not fixed | out of scope by construction; noted in spec, no task |
| §9 files: 2 new scripts + smoke.sh + 1 test; kernel/config/contracts/manifests/check-arch untouched | Task 1, Task 5, File Structure |
| §10 dispatch (clean tarball, BASE capture, degrade clauses, stop criteria, reviewer re-runs) | dispatch prompt (separate artifact); BASE in Base section; Task 6 Step 9 |
| Codex review R1: S4 real coverage; review.query assertion; unified fail diagnostics; pipe exit-code + `grep|head`; G1 scope; drift | Task 5; Task 4 Step 1; Task 2 Step 1; `grep -m1 -o`; Task 6 Step 8 `-- kernel`; S1 red text fixed, plan-not-in-diff noted |
| Codex review R2: spec S4 wording unified; every Task 6 step fail-closed (`rc=$?` capture, marker greps, `exit 1` on deviation); "quote paths with variables"; single `-timeout` wording | spec §3 Out + §2 aligned to §4; Task 6 Steps 1–8 rewritten; Global Constraints; Task 4 Step 2 |
| Codex review R3: evidence wording, prerequisite exit-code masking, S4 pointer naming, smoke orphan check, architecture marker, Python wording | Goal + Global Constraints; Task 5 Step 5; Task 6 Steps 2/4/6; Tech Stack; Task 6 Step 9 |

No open plan gaps after the R3 corrections above.

**2. Placeholder scan** — no TBD/TODO; every code step has literal file content or a literal patch; every run step has an exact command and expected output.

**3. Type/name consistency** — `$VQ`=local-cli, `$VD`=m1-dev throughout. `fail` defined in Task 2, used Tasks 2–5. `s2_run` defined+called in Task 3. `$RFAKE`/`$RREAL`/`$S3_WC` defined+used in Task 4. `TestRunPipelineGateFailOnStaleDiff` / `fakePipeline` / `approved` / `baseRun` / `runPipeline` match `pipeline_test.go` + `gate_test.go` + `pipeline.go`. Script markers `S1 OK`…`S3 OK` + `S4 SEAM OK` + `DONE-INTEGRITY QUALIFICATION: OK` consistent between script, run steps, and Task 6 greps. CLI substrings (`did not grant`, `outcome GATE_FAILED`, `outcome TIMEOUT`, `status APPROVED`, `status DONE`, `reason test:`, `reason build:`, `stage WAITING_REVIEW`, `"review_id":"`, `"status":"APPROVED"`, `"status":"PENDING"`) match the CLI-shapes table and `cli/vibe/main.go` / the review response shape. `restart_kernel` / `build_bins` / `kill_kernel_tree` names match between library and callers.
