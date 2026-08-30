# M1.7 — Adversarial DONE-Integrity Qualification — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prove — with a live kernel, driven as the external identity `local-cli` — that the four §4.4 attacks cannot get a Task to `DONE`, by adding a standalone qualification harness. No new enforcement code.

**Architecture:** One new library script `scripts/lib/kernel-harness.sh` (kernel lifecycle, lifted verbatim out of `scripts/smoke.sh`) and one new standalone script `scripts/qualify-done-integrity.sh` that sources it, boots its own kernel against the real `config/m1-*.json`, and runs scenarios S1–S4 as `local-cli`, restarting the kernel after each and re-asserting. `scripts/smoke.sh` is refactored to source the shared library (behaviour unchanged). The harness is added as its own milestone-acceptance step; `scripts/check-arch.sh` is **not** touched.

**Tech Stack:** Bash (`set -euo pipefail`), the existing `.bin/vibe` / `.bin/vibe-kernel` / `.bin/vibe-raw` binaries, Go 1.x toolchain for the transient 致残对照 mutations. No new Go dependency, no Python.

**Spec:** `docs/superpowers/specs/2026-08-30-m1-7-done-integrity-qualification-design.md` (and, through it, `docs/M1-DESIGN.md` §2 G4 / §4.2 / §4.3 / §4.4 / §10 / §13).

## Global Constraints

- **G1 Kernel Purity:** no task modifies `kernel/` source. Check: `git diff --name-only "$BASE" HEAD -- kernel/internal kernel/cmd kernel/sdk` must be empty (`$BASE` captured below).
- **No new external Go modules.**
- **No policy loosening.** `config/m1-policy.json` ships unchanged. It is edited only *transiently* inside Task 5's 致残对照 and restored in the same task.
- **No test-only hook in production code.** `runPipeline` / handlers get no new flags or branches for the harness. S4's live dimension rides on the S3 mechanic.
- **`scripts/check-arch.sh` is not modified.** It stays static and ~1 s.
- **Do NOT touch `docs/M1-DESIGN.md`.** Not staged, not edited, not committed. §13 is the reviewer's post-merge step.
- **Module paths:** kernel `github.com/example/agent-native-microkernel`; plugins `github.com/example/agent-native-os/plugins`; CLI `github.com/example/agent-native-os/cli`.
- **Assertion discipline:** every check captures command output into a variable and matches with `case "$var" in *"..."*)`. Never `cmd | grep -q` — under `set -o pipefail` a fast `grep -q` SIGPIPEs the producer and trips the pipeline (the flake M1.5 Task 8 and M1.6 hit).
- **Number discipline:** contract/manifest counts are unchanged at **31 contracts / 10 manifests**. If `check-arch.sh` prints different numbers, trust the command, note it, do not "fix" anything.
- **Commit trailer** — every commit message ends with exactly:
  ```
  Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
  Plan: docs/superpowers/plans/2026-08-30-m1-7-done-integrity-qualification.md
  ```
- **Commit identity:** author `ada <oashasu@gmail.com>` (the connector may substitute its own — known limit, not a deviation).
- **`go build ./cli/...` may drop a `./vibe` binary in cwd** — delete it, do not commit (`.gitignore` already has `/vibe`).

**Base:** branch `chatgpt/m1-7-done-integrity-qualification` from the tarball snapshot commit. Before Task 1: `BASE=$(git rev-parse HEAD)` — use `$BASE` for every G1 check. Do not hardcode a SHA.

---

## Background the executor needs

**The two identities** (`config/m1-policy.json`):

- `local-cli` — the **qualification / attacker** identity. Grants: `work.create@1`, `work.get@1`, `workflow.engineering.run@1`, `workflow.engineering.get@1`, `review.request@1`, `review.decide@1`, `review.get@1`, `review.query@1`, various `*.get@1`/`*.query@1`, `event.journal.replay@1`. **No `work.transition@1`** — direct or delegated-out. It reaches `work.transition` only *inside* the `workflow.engineering.run@1` delegation scope, i.e. only when the workflow plugin calls it on the caller's behalf.
- `m1-dev` — trusted dev identity with broad direct grants, used by the per-plugin fragment smokes. The harness uses it only for setup (`task create`) and status reads.

**The DONE gate** (`plugins/engineering-workflow/gate.go`, `doneGate`): first-failure-wins conjunction — `build == PASS`, `test == PASS`, `review.Status == APPROVED`, `review.DiffArtifactID == currentDiffArtifactID`, `len(AcceptanceResults) > 0`, every `result.Satisfied`. Returns `(false, "test: outcome FAIL")` etc. `runPipeline` calls it once, just before `work.transition(DONE)`, and on failure returns `Outcome: "GATE_FAILED", Reason: <why>`.

**The pipeline and reviews** (`plugins/engineering-workflow/pipeline.go`, `runPipeline`): always calls `ReviewRequest` itself, stores the returned id in `res.ReviewID`, and polls **only that id** via `ReviewGet` until `Status != "PENDING"`. A pre-existing `APPROVED` review for the same WorkContext is never looked at. On `ctx` deadline anywhere, returns `Outcome: "TIMEOUT"`.

**CLI output shapes** (`cli/vibe/main.go`) — match these exact substrings:

| command | stdout | on failure |
|---|---|---|
| `vibe task create -title T -goal G -repo R -ac ID=TEXT` | `task <id>  wc <id>  status PLANNED  version 1` | — |
| `vibe task show <task-id>` | multi-line incl. `status <STATUS>` on its own line | — |
| `vibe task transition <wc-id> -to <S> -expected-version <n>` | `status <S>  version <n>` | stderr: `... host policy did not grant external identity local-cli permission for work.transition@1`, exit 1 |
| `vibe review request <wc-id> -diff-artifact <id> [-evidence k:o]...` | `review <id>  status PENDING` | — |
| `vibe review decide <rev-id> -approved -reviewer X -acceptance ID=pass` | `review <id>  status APPROVED` | — |
| `vibe workflow run <task-id> -prompt P [-build C] [-test C] [-review-poll-ms N] [-mock-write-file F] [-mock-write-content S] [-timeout D]` | line 1 `outcome <O>  task <id>  reason <R>`; line 2 `work_context <id>  agent_run <id>  diff <id>  review <id>  session <id>`; line 3 `build_tool_run <id>  test_tool_run <id>  events <n>` | returns error `workflow outcome <O>: <R>` and **exits 1 whenever `<O> != DONE`** |
| `vibe workflow show <task-id> [-json]` | `stage <S>  outcome <O>  events <n>` then indented event types; `-json` prints the raw payload (contains `"review_id":"..."` inside the `review.requested` event) | — |

Because `vibe workflow run` exits non-zero on any non-DONE outcome, always invoke it as `$VQ workflow run ... 2>&1 || true` (foreground) or capture `$?` after `wait` (background), and assert on the captured text.

**Kernel restart in a script:** `restart_kernel` (from the shared library) SIGKILLs the kernel process tree, waits for the socket, then probes `event.journal.replay` + `blob.stat` until both answer. Call it directly (not in a subshell).

---

## File Structure

New:

- `scripts/lib/kernel-harness.sh` — kernel lifecycle for live-integration scripts. Sets `DATA` / `SOCK` / `VIBE_DATA_ROOT` / `TOKEN` / `DEV_TOKEN` / `VIBE_CLIENT_TOKEN` / `KPID`; defines `build_bins`, `kill_kernel_tree`, `restart_kernel`; exports them; installs the EXIT trap that kills the tree and removes `DATA`. Sourced *after* the caller has `cd`-ed to the repo root. One responsibility: kernel process lifecycle + its env.
- `scripts/qualify-done-integrity.sh` — the M1.7 qualification. Sources the library, boots one kernel, defines `$VQ` (local-cli) and `$VD` (m1-dev), builds a throwaway git repo, runs S1→S4 with a `restart_kernel` + re-assert after each stateful scenario, prints `DONE-INTEGRITY QUALIFICATION: OK`. One responsibility: the four §4.4 attacks.

Modified:

- `scripts/smoke.sh` — replace the inline kernel-lifecycle block (current lines 5–61) with `source scripts/lib/kernel-harness.sh` + `build_bins`. No behavioural change; the body (M1.1 checks, `source scripts/smoke-*.sh`, `echo "M1 SMOKE: PASSED"`) is untouched.

Untouched: `kernel/`, `plugins/**` (except Task 5's transient mutations), `config/**` (except Task 5's transient mutation), `contracts/**`, `plugins/manifests/**`, `scripts/check-arch.sh`, `cli/**`, `docs/M1-DESIGN.md`.

---

## Task 1: Extract the shared kernel-harness library

**Files:**
- Create: `scripts/lib/kernel-harness.sh`
- Modify: `scripts/smoke.sh` (lines 1–63 region)

**Interfaces:**
- Produces: a sourceable file that, once sourced from repo root, provides shell vars `DATA`, `SOCK`, `TOKEN` (`m1-local-cli-token`), `DEV_TOKEN` (`m1-dev-token`), `KPID`, and shell functions `build_bins` (runs `bash scripts/build.sh >/dev/null`), `kill_kernel_tree <pid>`, `restart_kernel` (boots `.bin/vibe-kernel` against `config/m1-*.json` + `contracts/`, waits for query-readiness, `exit 1` on failure). Installs `trap 'kill_kernel_tree "${KPID:-}"; rm -rf "$DATA"' EXIT`.
- Consumes: nothing from other tasks.

- [ ] **Step 1: Baseline — smoke is green before the refactor**

Run: `bash scripts/smoke.sh 2>&1 | tail -3`
Expected: `M1.6 WORKFLOW SMOKE: OK` and `M1 SMOKE: PASSED`, exit 0.
Also run: `pgrep -f 'vibe-kernel|plugins/manifests' | wc -l` → `0` (no orphans left behind).

- [ ] **Step 2: Create `scripts/lib/kernel-harness.sh`**

Create the file with exactly this content (the function bodies are lifted verbatim from `scripts/smoke.sh` — do not "improve" them):

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

`scripts/smoke.sh` currently (lines 1–14, then helpers through line 63) reads:

```bash
#!/usr/bin/env bash
# M1 smoke: real kernel + foundation/domain plugins, including restart survival.
set -euo pipefail
cd "$(dirname "$0")/.."
bash scripts/build.sh >/dev/null

DATA="$(mktemp -d)"
...
export SOCK DATA TOKEN KPID
export -f restart_kernel
trap 'kill_kernel_tree "${KPID:-}"; rm -rf "$DATA"' EXIT

restart_kernel
```

Replace everything from `bash scripts/build.sh >/dev/null` (line 5) through the `trap ... EXIT` line (the last line before the blank line + `restart_kernel`) with:

```bash
source scripts/lib/kernel-harness.sh
build_bins
```

Result — `scripts/smoke.sh` now starts:

```bash
#!/usr/bin/env bash
# M1 smoke: real kernel + foundation/domain plugins, including restart survival.
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/lib/kernel-harness.sh
build_bins

restart_kernel

append_out="$(.bin/vibe-raw -socket "$SOCK" -identity m1-dev -token "$DEV_TOKEN" \
  ...
```

Everything from `restart_kernel` onward (the `append_out=` block, the M1.1 checks, the five `source scripts/smoke-*.sh` lines, `echo "M1 SMOKE: PASSED"`) stays byte-for-byte as it was.

- [ ] **Step 4: Smoke is still green — run it 3×**

Run: `for i in 1 2 3; do bash scripts/smoke.sh >/tmp/m17-smoke-$i.log 2>&1 && echo "run $i: $(tail -1 /tmp/m17-smoke-$i.log)" || { echo "run $i FAILED"; tail -20 /tmp/m17-smoke-$i.log; exit 1; }; done`
Expected: three lines `run N: M1 SMOKE: PASSED`.
Then: `grep -c 'M1.6 WORKFLOW SMOKE: OK' /tmp/m17-smoke-*.log` → `1` per file; `grep -c FAIL /tmp/m17-smoke-*.log` → `0` per file.
Then orphan check: `pgrep -f 'vibe-kernel|plugins/manifests' | wc -l` → `0`.

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

## Task 2: Qualification skeleton + S1 (external direct transition denied)

**Files:**
- Create: `scripts/qualify-done-integrity.sh`

**Interfaces:**
- Consumes: `scripts/lib/kernel-harness.sh` (`build_bins`, `restart_kernel`, `$DATA`, `$SOCK`, `$TOKEN`, `$DEV_TOKEN`).
- Produces: an executable script that exits 0 and prints `DONE-INTEGRITY QUALIFICATION: OK` when every scenario holds, and exits 1 with a `S<n> FAIL: ...` line + the offending output + the kernel log tail otherwise. Later tasks insert S2/S3/S4 blocks *before* the final `echo`.

- [ ] **Step 1: Write the script with the skeleton and S1 only**

Create `scripts/qualify-done-integrity.sh` with exactly:

```bash
#!/usr/bin/env bash
# M1.7 adversarial DONE-integrity qualification. Drives the LIVE kernel as the
# EXTERNAL identity (local-cli) and proves the four docs/M1-DESIGN.md §4.4
# attacks cannot reach DONE. Sibling of kernel/tests/integration/m05_qualification.py.
#
# Discipline: every assertion captures output into a variable and matches with
# `case` -- never `cmd | grep -q` (SIGPIPE trips pipefail; the flake M1.5/M1.6 hit).
# `vibe workflow run` exits non-zero on any non-DONE outcome, so it is always
# run as `... 2>&1 || true` and asserted on the captured text.
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/lib/kernel-harness.sh
build_bins
restart_kernel

VQ=".bin/vibe -socket $SOCK -identity local-cli -token $TOKEN"     # attacker / qualification identity
VD=".bin/vibe -socket $SOCK -identity m1-dev -token $DEV_TOKEN"    # trusted setup + status reads

# Throwaway source repo for the workflow's worktree.
SRC="$DATA/qsrc"; mkdir -p "$SRC"
git -C "$SRC" -c init.defaultBranch=main init -q
printf 'class Calc { int add(int a,int b){return a+b;} }\n' > "$SRC/Calc.java"
git -C "$SRC" add -A
git -C "$SRC" -c user.email=s@t -c user.name=s -c commit.gpgsign=false commit -q -m init

# ---------- S1: external identity has NO work.transition@1 (direct or delegated-out) ----------
created="$($VD task create -title "s1" -goal "g" -repo "$SRC" -ac AC1="x")"
S1_WC="$(printf '%s\n' "$created" | sed -n 's/.*wc \([^ ]*\).*/\1/p')"
[ -n "$S1_WC" ] || { echo "S1 FAIL: task create: $created"; exit 1; }
for target in IN_PROGRESS IN_REVIEW DONE FAILED; do
  out="$($VQ task transition "$S1_WC" -to "$target" -expected-version 1 2>&1 || true)"
  case "$out" in
    *"did not grant"*) : ;;
    *) echo "S1 FAIL: local-cli was NOT denied work.transition -to $target: $out"; exit 1 ;;
  esac
done
echo "S1 OK: external direct work.transition denied for every target state"

echo "DONE-INTEGRITY QUALIFICATION: OK"
```

Then: `chmod +x scripts/qualify-done-integrity.sh`.

- [ ] **Step 2: Run it — S1 passes**

Run: `bash scripts/qualify-done-integrity.sh 2>&1 | tail -5`
Expected: `S1 OK: external direct work.transition denied for every target state` then `DONE-INTEGRITY QUALIFICATION: OK`, exit 0.
Orphan check: `pgrep -f 'vibe-kernel|plugins/manifests' | wc -l` → `0`.

- [ ] **Step 3: Falsify S1 (致残对照) — grant `work.transition@1`, confirm red, revert**

Edit `config/m1-policy.json`: add `"work.transition@1",` to the `grants."local-cli".capabilities` array (first element is fine).

Run: `bash scripts/qualify-done-integrity.sh 2>&1 | tail -5`
Expected: **FAIL** — `S1 FAIL: local-cli was NOT denied work.transition -to IN_PROGRESS: status IN_PROGRESS  version 2` (the `IN_PROGRESS` transition now succeeds; the later targets report `illegal transition` instead of `did not grant`), exit 1.

Revert: `git checkout config/m1-policy.json`. Re-run: back to `DONE-INTEGRITY QUALIFICATION: OK`.

- [ ] **Step 4: Commit**

```bash
rm -f ./vibe
git add scripts/qualify-done-integrity.sh
git commit -m "$(cat <<'EOF'
test(m1.7): qualification harness skeleton + S1 (direct transition denied)

scripts/qualify-done-integrity.sh boots its own kernel via the shared
harness lib and, as the external identity local-cli, asserts
work.transition is denied for every target state. Falsified: granting
local-cli work.transition@1 turns S1 red.

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
- Consumes: `$VQ`, `$VD`, `$SRC`, `restart_kernel` from the script so far.
- Produces: an `s2_run` shell function + two calls; prints `S2 OK: ...`.

- [ ] **Step 1: Insert the S2 block**

Immediately before the final `echo "DONE-INTEGRITY QUALIFICATION: OK"` line, insert:

```bash
# ---------- S2: failed evidence cannot pass the DONE gate ----------
# Run the workflow with a failing build or test, approve the review anyway
# (a colluding human), and assert the gate still refuses DONE.
s2_run() {  # $1=build cmd  $2=test cmd  $3=expected reason substring
  local created task show j pid rev decide wf_out ts
  created="$($VD task create -title "s2" -goal "harden add" -repo "$SRC" -ac AC1="build+test pass")"
  task="$(printf '%s\n' "$created" | sed -n 's/^task \([^ ]*\).*/\1/p')"
  [ -n "$task" ] || { echo "S2 FAIL: task create: $created"; exit 1; }
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
      rev="$(printf '%s\n' "$j" | grep -o '"review_id":"[^"]*"' | head -1 | sed 's/.*:"//;s/"//')"
      [ -n "$rev" ] && break ;;
    esac
    sleep 0.1
  done
  [ -n "$rev" ] || { echo "S2 FAIL: never reached WAITING_REVIEW ($1 / $2)"; cat "$DATA/s2.out"; exit 1; }
  decide="$($VQ review decide "$rev" -approved -reviewer mallory -acceptance AC1=pass 2>&1 || true)"
  case "$decide" in *"status APPROVED"*) : ;; *) echo "S2 FAIL: review decide: $decide"; exit 1 ;; esac
  if wait "$pid"; then echo "S2 FAIL: workflow run exited 0 with $1 / $2"; cat "$DATA/s2.out"; exit 1; fi
  wf_out="$(cat "$DATA/s2.out")"
  case "$wf_out" in *"outcome GATE_FAILED"*) : ;; *) echo "S2 FAIL: outcome not GATE_FAILED ($1 / $2): $wf_out"; exit 1 ;; esac
  case "$wf_out" in *"$3"*) : ;; *) echo "S2 FAIL: reason lacks '$3': $wf_out"; exit 1 ;; esac
  ts="$($VD task show "$task" 2>&1 || true)"
  case "$ts" in *"status DONE"*) echo "S2 FAIL: task went DONE despite $1 / $2: $ts"; exit 1 ;; esac
  restart_kernel
  ts="$($VD task show "$task" 2>&1 || true)"
  case "$ts" in *"status DONE"*) echo "S2 FAIL: task DONE after restart: $ts"; exit 1 ;; esac
}
s2_run "sh -c true"  "sh -c false" "reason test:"
s2_run "sh -c false" "sh -c true"  "reason build:"
echo "S2 OK: failing test and failing build both blocked at the gate"
```

- [ ] **Step 2: Run it — S1 + S2 pass**

Run: `bash scripts/qualify-done-integrity.sh 2>&1 | tail -6`
Expected: `S1 OK: ...`, `S2 OK: failing test and failing build both blocked at the gate`, `DONE-INTEGRITY QUALIFICATION: OK`, exit 0.

- [ ] **Step 3: Falsify S2 (致残对照) — remove the test-PASS check, confirm red, revert**

In `plugins/engineering-workflow/gate.go`, delete these three lines from `doneGate`:

```go
	if test.Outcome != "PASS" {
		return false, fmt.Sprintf("test: outcome %s", test.Outcome)
	}
```

Run: `bash scripts/qualify-done-integrity.sh 2>&1 | tail -8`
Expected: **FAIL** — the first `s2_run` (`sh -c true` / `sh -c false`) now gets `outcome DONE` and the workflow run exits 0, so `S2 FAIL: workflow run exited 0 with sh -c true / sh -c false`, exit 1.

Revert: `git checkout plugins/engineering-workflow/gate.go`. Re-run: `S2 OK` again.

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

## Task 4: S3 — injected/stale review never consulted (+ S4 coverage note)

**Files:**
- Modify: `scripts/qualify-done-integrity.sh` (insert an S3 block and an S4 note before the final `echo`)

**Interfaces:**
- Consumes: `$VQ`, `$VD`, `$SRC`, `restart_kernel`.
- Produces: prints `S3 OK: ...` and `S4 OK: ...`.

- [ ] **Step 1: Insert the S3 block + S4 note**

Immediately before the final `echo "DONE-INTEGRITY QUALIFICATION: OK"` line, insert:

```bash
# ---------- S3: an injected APPROVED review does not move the Task ----------
# local-cli holds review.request@1 + review.decide@1 directly, so it can
# fabricate an APPROVED review for the WorkContext BEFORE the workflow runs.
# The pipeline must ignore it: it polls only the review id IT created.
created="$($VD task create -title "s3" -goal "harden add" -repo "$SRC" -ac AC1="build+test pass")"
S3_TASK="$(printf '%s\n' "$created" | sed -n 's/^task \([^ ]*\).*/\1/p')"
S3_WC="$(printf '%s\n' "$created" | sed -n 's/.*wc \([^ ]*\).*/\1/p')"
[ -n "$S3_TASK" ] && [ -n "$S3_WC" ] || { echo "S3 FAIL: task create: $created"; exit 1; }

fake_out="$($VQ review request "$S3_WC" -diff-artifact art-injected-not-real \
             -evidence build:PASS -evidence test:PASS 2>&1 || true)"
RFAKE="$(printf '%s\n' "$fake_out" | sed -n 's/^review \([^ ]*\).*/\1/p')"
[ -n "$RFAKE" ] || { echo "S3 FAIL: could not create injected review: $fake_out"; exit 1; }
fdec="$($VQ review decide "$RFAKE" -approved -reviewer mallory -acceptance AC1=pass 2>&1 || true)"
case "$fdec" in *"status APPROVED"*) : ;; *) echo "S3 FAIL: could not approve injected review: $fdec"; exit 1 ;; esac

# Real workflow; real review left undecided; short deadline so the poll loop times out.
s3_out="$($VQ workflow run "$S3_TASK" -prompt "harden" -build "sh -c true" -test "sh -c true" \
           -review-poll-ms 200 -mock-write-file Calc.java -mock-write-content '// s3
' -timeout 20s 2>&1 || true)"
case "$s3_out" in *"outcome TIMEOUT"*) : ;; *) echo "S3 FAIL: expected TIMEOUT, got: $s3_out"; exit 1 ;; esac
RREAL="$(printf '%s\n' "$s3_out" | sed -n 's/.*  review \([^ ]*\)  session.*/\1/p')"
{ [ -n "$RREAL" ] && [ "$RREAL" != "$RFAKE" ]; } || { echo "S3 FAIL: workflow did not open its own review (real='$RREAL' fake='$RFAKE'): $s3_out"; exit 1; }
ts="$($VD task show "$S3_TASK" 2>&1 || true)"
case "$ts" in *"status DONE"*) echo "S3 FAIL: task DONE despite undecided real review: $ts"; exit 1 ;; esac
restart_kernel
ts="$($VD task show "$S3_TASK" 2>&1 || true)"
case "$ts" in *"status DONE"*) echo "S3 FAIL: task DONE after restart: $ts"; exit 1 ;; esac
echo "S3 OK: injected APPROVED review is never consulted; undecided real review => TIMEOUT, not DONE"

# ---------- S4: wrong-diff approval ----------
# Predicate: plugins/engineering-workflow/gate_test.go::TestDoneGateFailsOnEachCondition
#   case "wrong diff" — review.DiffArtifactID != currentDiffArtifactID => gate rejects.
# Live binding path: S3 above already exercises it — R_fake carries a non-current
#   diff_artifact_id ("art-injected-not-real") and is never consulted by the pipeline.
echo "S4 OK: wrong-diff predicate covered by gate_test.go; live binding path covered by S3"
```

- [ ] **Step 2: Run it — S1–S4 pass**

Run: `bash scripts/qualify-done-integrity.sh 2>&1 | tail -8`
Expected: `S1 OK`, `S2 OK`, `S3 OK: injected APPROVED review is never consulted...`, `S4 OK: ...`, `DONE-INTEGRITY QUALIFICATION: OK`, exit 0.

If `S3 FAIL: workflow did not open its own review (real='' ...)` appears, the 20 s deadline fired before the pipeline created its review on this machine — raise both `-timeout 20s` occurrences to `40s` and re-run. Log this as a degradation; it is not a stop.

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

Run: `bash scripts/qualify-done-integrity.sh 2>&1 | tail -8`
Expected: **FAIL** — the pipeline's poll of its own (PENDING) review now resolves to `R_fake` (APPROVED), the poll loop breaks, and `doneGate` rejects on the diff mismatch, so the outcome is `GATE_FAILED` not `TIMEOUT`: `S3 FAIL: expected TIMEOUT, got: ... outcome GATE_FAILED ... reason diff: ...`, exit 1.
(Note the defence in depth this exposes: even with the poll fooled, the `diff_artifact_id` binding still blocks `DONE` — the task never actually reaches `DONE`, only the outcome label changes. S3 asserts on `outcome == TIMEOUT` precisely so it still falsifies.)

Revert: `git checkout plugins/review/handlers.go`. Re-run: `S3 OK` again.

- [ ] **Step 4: Falsify S4's predicate (致残对照) at the unit layer**

In `plugins/engineering-workflow/gate.go`, delete from `doneGate`:

```go
	if review.DiffArtifactID != currentDiffArtifactID {
		return false, fmt.Sprintf("diff: reviewed %s != current %s", review.DiffArtifactID, currentDiffArtifactID)
	}
```

Run: `go test ./plugins/engineering-workflow/ -run TestDoneGateFailsOnEachCondition -v`
Expected: **FAIL** on the `wrong diff` case (`ok=true` where `false` was wanted).

Revert: `git checkout plugins/engineering-workflow/gate.go`. Re-run the test → PASS.

- [ ] **Step 5: Commit**

```bash
rm -f ./vibe
git add scripts/qualify-done-integrity.sh
git commit -m "$(cat <<'EOF'
test(m1.7): S3 — injected/stale review never consulted; S4 coverage note

local-cli fabricates an APPROVED review for the WorkContext before the
workflow runs; the pipeline ignores it and, with its own review left
undecided, times out rather than reaching DONE — verified across a kernel
restart. Falsified: making review.get fall back to the WC's latest
decision turns S3 red. S4's predicate stays covered by gate_test.go
(wrong-diff case); its live path rides on S3's non-current diff id.

Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
Plan: docs/superpowers/plans/2026-08-30-m1-7-done-integrity-qualification.md
EOF
)"
```

---

## Task 5: Full acceptance + 致残对照 sweep

**Files:** none changed (verification only; re-verifies Tasks 1–4).

**Interfaces:**
- Consumes: the finished `scripts/qualify-done-integrity.sh` and `scripts/lib/kernel-harness.sh`.
- Produces: raw command output for the PR body; nothing committed unless a stray `./vibe` needs removing.

- [ ] **Step 1: Three-module build**

Run: `go build ./plugins/... ./cli/... && (cd kernel && go build ./...) && echo BUILD_OK`
Expected: `BUILD_OK`, exit 0.

- [ ] **Step 2: Go tests**

Run: `go test ./plugins/... ./plugins/_template ./cli/... && (cd kernel && go test ./...)`
Expected: all packages `ok` (or `no test files`), exit 0.

- [ ] **Step 3: Kernel regression untouched**

Run: `cd kernel && ./scripts/build.sh >/dev/null && python3 tests/integration/m05_qualification.py 2>&1 | tail -2`
Expected: `M0.5 ADVERSARIAL QUALIFICATION: PASSED`.

- [ ] **Step 4: Architecture checks — unchanged, still static**

Run: `bash scripts/check-arch.sh`
Expected: `CONTRACT CHECK: PASSED (31 contracts, ...)`, `COMPOSITION FITNESS: PASSED (10 manifests)`, `ARCHITECTURE FITNESS: PASSED`, `ARCH CHECKS OK`. (If the counts differ, trust the command and note it; M1.7 adds no contracts or manifests.)
Confirm `scripts/check-arch.sh` is byte-identical to `$BASE`: `git diff --stat "$BASE" HEAD -- scripts/check-arch.sh` → empty.

- [ ] **Step 5: DONE-integrity qualification — run it 3×**

Run: `for i in 1 2 3; do bash scripts/qualify-done-integrity.sh >/tmp/m17-qual-$i.log 2>&1 && echo "run $i: $(tail -1 /tmp/m17-qual-$i.log)" || { echo "run $i FAILED"; cat /tmp/m17-qual-$i.log; exit 1; }; done`
Expected: three lines `run N: DONE-INTEGRITY QUALIFICATION: OK`.
Then: `grep -hE '^S[1-4] OK' /tmp/m17-qual-1.log` → four lines S1–S4.
Orphan check: `pgrep -f 'vibe-kernel|plugins/manifests' | wc -l` → `0`.

- [ ] **Step 6: Smoke still green — run it 5×**

Run: `for i in 1 2 3 4 5; do bash scripts/smoke.sh >/tmp/m17-smoke5-$i.log 2>&1 && echo "run $i: $(tail -1 /tmp/m17-smoke5-$i.log)" || { echo "run $i FAILED"; tail -30 /tmp/m17-smoke5-$i.log; exit 1; }; done`
Expected: five `run N: M1 SMOKE: PASSED`; each log has one `M1.6 WORKFLOW SMOKE: OK` and zero `FAIL`.

- [ ] **Step 7: The full 致残对照, one sweep — each mutation red, then reverted**

Run each, confirm the stated failure, then `git checkout <file>` and confirm green:

| # | Mutation | Command that must go red | Expected red |
|---|---|---|---|
| M-S1 | add `"work.transition@1"` to `grants."local-cli".capabilities` in `config/m1-policy.json` | `bash scripts/qualify-done-integrity.sh` | `S1 FAIL: local-cli was NOT denied ... -to IN_PROGRESS: status IN_PROGRESS` |
| M-S2 | delete the `test.Outcome != "PASS"` block from `doneGate` in `plugins/engineering-workflow/gate.go` | `bash scripts/qualify-done-integrity.sh` | `S2 FAIL: workflow run exited 0 with sh -c true / sh -c false` |
| M-S3 | the `getHandler` fallback patch from Task 4 Step 3 in `plugins/review/handlers.go` | `bash scripts/qualify-done-integrity.sh` | `S3 FAIL: expected TIMEOUT, got: ... outcome GATE_FAILED` |
| M-S4 | delete the `review.DiffArtifactID != currentDiffArtifactID` block from `doneGate` | `go test ./plugins/engineering-workflow/ -run TestDoneGateFailsOnEachCondition` | FAIL on `wrong diff` |

After the sweep: `git status --porcelain` → empty (every mutation reverted).

- [ ] **Step 8: G1 anchors empty**

Run:
```bash
git diff --name-only "$BASE" HEAD -- kernel/internal kernel/cmd kernel/sdk
git diff --name-only "$BASE" HEAD -- docs/M1-DESIGN.md
git diff --name-only "$BASE" HEAD
```
Expected: first two empty; the third lists only `scripts/lib/kernel-harness.sh`, `scripts/qualify-done-integrity.sh`, `scripts/smoke.sh`, `docs/superpowers/plans/2026-08-30-m1-7-done-integrity-qualification.md` (the plan travels in the tarball).

- [ ] **Step 9: Open the PR**

Branch `chatgpt/m1-7-done-integrity-qualification` → `main`, title **M1.7 — Adversarial DONE-Integrity Qualification**. Body: the 4-commit table; the raw output of Steps 1–8; and the 致残对照 sweep results (which mutation, which command, the exact red line, confirmed green after revert). State plainly that the reviewer will re-run all of it independently — self-report is not acceptance.

---

## Self-Review

**1. Spec coverage**

| Spec item | Task |
|---|---|
| §3 In: `scripts/lib/kernel-harness.sh` extracted from smoke.sh, smoke unchanged | Task 1 |
| §3 In: `scripts/qualify-done-integrity.sh` standalone, own kernel, S1–S4, restart+re-assert, `DONE-INTEGRITY QUALIFICATION: OK` | Tasks 2–4 |
| §3 In: milestone acceptance gains a step; check-arch.sh untouched | Task 5 Steps 4–5; Global Constraints |
| §3 Out: no kernel / no new Go dep / no policy loosening / no production test hook / no check-arch.sh change | Global Constraints; Task 5 Step 4 & Step 8 |
| §4 S1 (four target states, `did not grant`) | Task 2 Step 1 |
| §4 S2 (failing test + failing build, approve anyway, `GATE_FAILED` + reason, not DONE, restart) | Task 3 Step 1 |
| §4 S3 (inject APPROVED review, real review undecided, `TIMEOUT`, own review id ≠ fake, not DONE, restart) | Task 4 Step 1 |
| §4 S4 (predicate → gate_test.go; live path → S3 mechanic; **no** production flag) | Task 4 Step 1 + Step 4 |
| §5 robustness: EXIT trap, bounded polling not fixed sleeps, `-timeout` is the ctx deadline, raise-don't-sleep degradation | Task 1 Step 2 (trap); Task 3/4 Step 1 (bounded loops); Task 4 Step 2 (degradation note) |
| §6 致残对照: M-S1 policy, M-S2 gate test-check, M-S3 review getHandler, plus S4 predicate at unit layer | Task 2 Step 3; Task 3 Step 3; Task 4 Steps 3–4; Task 5 Step 7 |
| §7 acceptance step order (build / go test / m05 / check-arch / qualify / smoke×5 / G1) | Task 5 Steps 1–8 |
| §8 observation (empty diff → DONE) not fixed | out of scope by construction; noted in spec, no task |
| §9 files: 2 new + smoke.sh modified; kernel/config/contracts/manifests/check-arch untouched | Task 1 & File Structure |
| §10 dispatch (clean tarball, BASE capture, degrade clauses, stop criteria, reviewer re-runs) | dispatch prompt (separate artifact); BASE in Base section; Task 5 Step 9 |

No gaps.

**2. Placeholder scan** — no TBD/TODO; every code step has literal file content or a literal patch; every run step has an exact command and expected output. Clear.

**3. Type/name consistency** — `$VQ` = local-cli, `$VD` = m1-dev throughout. `$RFAKE`/`$RREAL` defined and used in Task 4 only. `s2_run` defined and called in Task 3 only. Script markers `S1 OK` … `S4 OK` and final `DONE-INTEGRITY QUALIFICATION: OK` are consistent between the script, the run steps, and Task 5's greps. CLI substrings (`did not grant`, `outcome GATE_FAILED`, `outcome TIMEOUT`, `status APPROVED`, `status DONE`, `reason test:`, `reason build:`, `stage WAITING_REVIEW`, `"review_id":"`) all match the CLI shapes table and `cli/vibe/main.go`. `restart_kernel` / `build_bins` / `kill_kernel_tree` names match between the library and both callers.
