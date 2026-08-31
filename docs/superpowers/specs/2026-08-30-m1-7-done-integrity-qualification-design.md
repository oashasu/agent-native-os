# M1.7 — Adversarial DONE-Integrity Qualification — Design

**Status:** approved for planning (2026-08-30)
**Spec source:** `docs/M1-DESIGN.md` §2 (G4), §4.2 (policy + delegation), §4.3 (DONE gate conjunction), §4.4 (the four attacks), §10 (G4 independent qualification), §13 (milestone M1.7).
**Milestone position:** after M1.6 (`m1.6-engineering-workflow`, main `3832303`), before M1.8 (real provider).

---

## 1. Goal

Prove — with a **live kernel**, driven as the **external identity** — that a hostile
principal holding only the capabilities `local-cli` actually has cannot get a Task to
`DONE` by any path except the workflow's own gated one.

M1.6 already built every enforcement mechanism. M1.7 adds **no new enforcement**. It adds:

1. A live adversarial qualification harness (`scripts/qualify-done-integrity.sh`) — the
   engineering vertical's sibling to the kernel's `tests/integration/m05_qualification.py`.
2. Live coverage for the one attack (S3, injected/stale review) that M1.6's unit tests
   do not reach.

If the harness cannot be made to pass **without loosening a gate predicate or granting
the external identity a capability it should not have**, that is a load-bearing finding:
stop and report — G4 is not actually met.

## 2. What is already true (do not rebuild)

| Attack (§4.4) | Existing mechanism | Existing test |
|---|---|---|
| **S1** external direct `work.transition(<task>, DONE)` | `config/m1-policy.json`: `local-cli` has **no** `work.transition@1` grant (direct or delegated-out); it reaches `work.transition` only inside the `workflow.engineering.run@1` delegation scope | `scripts/smoke-workflow.sh` asserts the `IN_PROGRESS` denial as an "M1.7 preview" |
| **S2** failed build/test → DONE | `plugins/engineering-workflow/gate.go` `doneGate()` — first-failure-wins conjunction, rejects `build != PASS`, `test != PASS` | `gate_test.go::TestDoneGateFailsOnEachCondition` (all 5 branches) |
| **S3** stale / injected review → DONE | `runPipeline` always calls `ReviewRequest` itself and polls **only the review id it got back**; a pre-existing `APPROVED` review for the same WorkContext is never consulted | none (structural, untested) |
| **S4** wrong-diff approval → DONE | `doneGate()` rejects `review.DiffArtifactID != currentDiffArtifactID` | `gate_test.go::.../wrong diff` (unit only) |

**M1 has no rework loop** (explicit NON-GOAL, §11: "自动 workflow reconciler / 自动续跑 / rework 回环").
The workflow is single-attempt. "Stale review" in the full §4.3 sense ("diff 变了旧 review 失效")
is therefore only reachable in M1 by *constructing* a competing review out of band — which is
exactly what S3 does.

## 3. Scope

### In

- `scripts/lib/kernel-harness.sh` — kernel lifecycle helpers (`build_bins`,
  `restart_kernel`, `kill_kernel_tree`, cleanup trap, data-root + token env), **extracted
  verbatim from `scripts/smoke.sh`**. Pure refactor; `smoke.sh` sources it and keeps
  behaving identically (5/5 green, 0 orphan processes).
- `scripts/qualify-done-integrity.sh` — new, standalone. Sources the harness lib, boots its
  own kernel against `config/m1-{policy,bindings}.json`, runs S1–S3 live (plus an `S4 OK`
  pointer line), restarts the kernel after each stateful scenario and re-asserts, prints
  `DONE-INTEGRITY QUALIFICATION: OK` on success. A unified `fail()` helper prints the
  message + `kernel.log` tail on any failed assertion.
- `plugins/engineering-workflow/pipeline_test.go` — one new test for S4 (see below).
- Milestone acceptance list gains a step (see §7). `docs/M1-DESIGN.md` §13 marks M1.7 done —
  **reviewer's post-merge step, not the implementer's.**

### Out (explicitly not this milestone)

- **No kernel change.** G1 holds.
- **No new external Go dependency.**
- **No policy loosening.** `config/m1-policy.json` is not relaxed. (It *is* touched
  transiently during the 致残对照 in the acceptance task, then restored — see §6.)
- **No test-only hook in production code.** In particular, `runPipeline` gets **no**
  `-mock-review-diff-artifact`-style flag to force a live S4 diff mismatch. S4's live
  dimension is covered by the S3 mechanic (same `diff_artifact_id` binding path); its
  predicate is covered by the existing `gate_test.go` unit case. This split is written
  into the harness as a comment.
- **No `check-arch.sh` change.** It stays static and ~1 s. The qualification is a separate
  acceptance step, mirroring the kernel's static-checks vs `m05_qualification.py` split.
- Rework loop, `EvidenceRef.invalidated_at` population, empty-diff rejection — none of these
  are M1.7. (Empty diff is registered as an observation, §8.)

## 4. The harness

`scripts/qualify-done-integrity.sh`, run from the repo root. Structure mirrors
`smoke-workflow.sh`: every assertion captures command output into a variable and matches
with `case "$var" in *"..."*)` — never `cmd | grep -q` (that SIGPIPEs the producer under
`set -o pipefail`; the flake class M1.5 Task 8 and M1.6 hit).

Two identities, same as `smoke-workflow.sh`:

- `VQ` = `.bin/vibe -socket $SOCK -identity local-cli -token $TOKEN` — the **attacker** /
  qualification identity. Every attack is issued through `VQ`.
- `VD` = `.bin/vibe -socket $SOCK -identity m1-dev -token $DEV_TOKEN` — trusted setup
  (task creation, status reads that `local-cli` cannot do).

### S1 — external direct transition is ungranted

As `VQ`, for each target state in `IN_PROGRESS`, `IN_REVIEW`, `DONE`, `FAILED`:

```
$VQ task transition <wc> -to <state> -expected-version <v>   → stderr contains "did not grant"
```

All four denied — `local-cli` has no `work.transition@1` capability at all, so this fails at
the authz layer before any state-machine check. (`smoke-workflow.sh` keeps its single-state
preview assertion; this is the full version.)

### S2 — failed evidence cannot pass the gate

Create a task via `VD`. As `VQ`, run the workflow with a **failing test**, then approve the
review anyway (a lenient/colluding human):

```
$VQ workflow run <task> -build "sh -c true" -test "sh -c false" \
    -review-poll-ms 200 -mock-write-file X -mock-write-content '...' -timeout 3m   (background)
# wait for stage WAITING_REVIEW, find the PENDING review id from `workflow show -json`
$VQ review decide <rev> -approved -reviewer mallory -acceptance AC1=pass
wait $PID
```

Assert: `outcome GATE_FAILED`, reason string contains `test`; `$VD task show` → status is
**not** `DONE` (it is `IN_REVIEW`); `$VQ workflow show` → outcome not `DONE`.
`restart_kernel`; re-assert status still not `DONE`.

Then the **failing-build** variant: `-build "sh -c false" -test "sh -c true"` → reason
contains `build`, same not-DONE assertions.

### S3 — an injected APPROVED review does not move the Task

As `VQ` (who holds `review.request@1` + `review.decide@1` directly), fabricate an approved
review for the WorkContext **before** running the workflow:

```
$VQ review request <wc> -diff-artifact art-injected-does-not-exist -evidence build:PASS -evidence test:PASS
    → review R_fake, status PENDING
$VQ review decide R_fake -approved -reviewer mallory -acceptance AC1=pass
    → R_fake, status APPROVED
```

Now run the workflow with a **short-ish timeout** (`-timeout 20s` — long enough to get past
`ReviewRequest`, short enough that the review poll then times out) and **never decide the
real review**:

```
$VQ workflow run <task> -build "sh -c true" -test "sh -c true" \
    -review-poll-ms 200 -mock-write-file X -mock-write-content '...' -timeout 20s   (foreground)
```

Assert:
- `outcome TIMEOUT` (the pipeline blocked at WAITING_REVIEW polling **its own** review, not
  `R_fake`).
- the `review` id in the run's output is non-empty and `!= R_fake` — the workflow opened its
  own review.
- `review.query{work_context_id}` via `.bin/vibe-raw` returns a payload containing **both**
  `R_fake` and `R_real`, with a `"status":"APPROVED"` and a `"status":"PENDING"` present.
- `$VD task show` → status not `DONE`, even though an `APPROVED` review exists for this WC.
- `restart_kernel`; re-assert not `DONE`.

### S4 — wrong-diff approval

A genuinely live S4 — the workflow's **own** review carrying a stale diff — has **no code
path in M1**: `runPipeline` passes one `diff` artifact id to both `ReviewRequest` and
`doneGate`, and nothing mutates it (no rework loop). Codex review confirmed the "bind a real
foreign diff to a fabricated review" idea does not help either — the pipeline never consults
a review it did not create (that is S3). So S4 is proven one seam in, at `runPipeline`:

1. **Pipeline integration** — new `plugins/engineering-workflow/pipeline_test.go::TestRunPipelineGateFailOnStaleDiff`:
   drive `runPipeline` with fake capability closures where `CollectDiff` returns `"art-1"`
   and the only review is `APPROVED` but bound to `"art-STALE"`. Assert `Outcome ==
   "GATE_FAILED"`, `Reason` contains `diff`, and no `transition:DONE`. Strictly more than
   the predicate test — it exercises the real `runPipeline → doneGate` composition.
2. **Predicate** — existing `gate_test.go::TestDoneGateFailsOnEachCondition` case
   `"wrong diff"`.

The harness echoes an `S4 OK` line pointing at both tests and stating the live path does not
exist in M1; it runs no S4 assertion of its own.

### Tail

```
echo "DONE-INTEGRITY QUALIFICATION: OK"
```

Every failed assertion routes through a `fail "<msg>"` helper that echoes the message, dumps
`tail -n 40 "$DATA/kernel.log"`, and `exit 1`.

## 5. Error handling / robustness

- `set -euo pipefail`; `trap` in the harness lib kills the kernel process tree on exit
  (EXIT, INT, TERM) — the qualification must not leak plugin processes any more than smoke does.
- Unified `fail()` helper: message + `kernel.log` tail + `exit 1` on every failed assertion.
- WAITING_REVIEW / timeout polling uses bounded loops with backoff, never a fixed `sleep`
  to "wait long enough" — matches `smoke-workflow.sh`. First-match extraction uses
  `grep -m1 -o`, never `grep -o … | head -1` (SIGPIPE under `pipefail`).
- The `-timeout 20s` value in S3 is the deadline the pipeline watches (`ctx`), not a
  shell timeout. If it proves flaky on a slow sandbox (TIMEOUT fires before the review is
  created → empty `R_real`), raise it to `40s` — do not switch to a fixed sleep. This is a
  degrade-and-log item, not a stop.

## 6. Testing — 致残对照 (falsification)

The qualification is only meaningful if it can be made to fail. The acceptance task performs
each mutation, confirms the named scenario goes red, and restores it:

| # | Mutation | Command | Expected red |
|---|---|---|---|
| M-S1 | add `"work.transition@1"` to `grants."local-cli".capabilities` in `config/m1-policy.json` | `qualify-done-integrity.sh` | **S1** — first loop iteration (`-to IN_PROGRESS`) now returns `status IN_PROGRESS  version 2`; `fail "S1 FAIL: … was NOT denied … -to IN_PROGRESS"` |
| M-S2 | delete the `test.Outcome != "PASS"` block from `doneGate` in `gate.go` | `qualify-done-integrity.sh` | **S2** — the failing-test run reaches `outcome DONE`, `vibe workflow run` exits 0; `fail "S2 FAIL: workflow run exited 0 …"` |
| M-S3 | in `plugins/review/handlers.go` `getHandler`, when the requested review is still `PENDING`, fall back to the most-recently-decided review for the same `work_context_id` | `qualify-done-integrity.sh` | **S3** — the pipeline's poll resolves to `R_fake`; `outcome` becomes `GATE_FAILED` (diff mismatch) not `TIMEOUT`; `fail "S3 FAIL: expected TIMEOUT …"` |
| M-S4 | delete the `review.DiffArtifactID != currentDiffArtifactID` block from `doneGate` in `gate.go` | `go test ./plugins/engineering-workflow/ -run 'TestRunPipelineGateFailOnStaleDiff|TestDoneGateFailsOnEachCondition'` | **both tests FAIL** — pipeline seam yields `Outcome == "DONE"`; predicate case `"wrong diff"` fails |

Note the **defence in depth** M-S3 exposes: even when the poll is fooled into adopting
`R_fake`, `doneGate`'s `diff_artifact_id` binding still blocks `DONE` — so the task never
actually reaches `DONE`, only the *outcome label* changes. S3's assertion is written on
`outcome == TIMEOUT` (and both review ids present via `review.query`) precisely so it still
falsifies.

Each mutation reverted (`git checkout <file>`); full qualification green again. The mutation
list and its results go in the PR body — self-report is not acceptance; the reviewer re-runs
it independently.

## 7. Acceptance step wiring

Milestone acceptance (the implementer runs, pastes raw output into the PR):

```
1. three-module build            → exit 0
2. go test (plugins + cli + kernel)  → all ok (incl. TestRunPipelineGateFailOnStaleDiff)
3. kernel regression             → m05_qualification.py PASSED
4. architecture checks           → check-arch.sh  (unchanged: static, ~1 s, 31 contracts / 10 manifests)
5. DONE-integrity qualification  → bash scripts/qualify-done-integrity.sh ×3  → DONE-INTEGRITY QUALIFICATION: OK   ← NEW
6. smoke ×5                       → each: M1.6 WORKFLOW SMOKE: OK + M1 SMOKE: PASSED, 0 FAIL, 0 orphans
7. G1 anchors empty              → git diff --name-only "$BASE" HEAD -- kernel  (and -- docs/M1-DESIGN.md); full diff lists only the 4 changed files
```

`scripts/check-arch.sh` is **not** modified. Future milestone plans/dispatch prompts pick up
step 5 from here.

## 8. Registered observations (not fixed in M1.7)

- **Empty diff reaches DONE.** If the mock agent writes nothing, `artifact.collect_diff`
  yields a 0-file artifact; `doneGate` has no `files_changed > 0` predicate, so an approved
  no-op change can transition to `DONE`. §4.3 does not forbid it. Candidate hardening for a
  later milestone; out of scope here.

## 9. Files

New:
- `scripts/lib/kernel-harness.sh`
- `scripts/qualify-done-integrity.sh`
- `docs/superpowers/plans/2026-08-30-m1-7-done-integrity-qualification.md` (the plan, committed before the branch is cut)

Modified:
- `scripts/smoke.sh` — source `scripts/lib/kernel-harness.sh` instead of defining the
  helpers inline; no behavioural change.
- `plugins/engineering-workflow/pipeline_test.go` — `+TestRunPipelineGateFailOnStaleDiff`.

Untouched: `kernel/`, `config/m1-policy.json` (except the transient 致残对照 mutation),
`plugins/engineering-workflow/gate.go` + `plugins/review/handlers.go` (except transient 致残对照
mutations), `docs/M1-DESIGN.md` (reviewer's §13 bump post-merge), `scripts/check-arch.sh`.

## 10. Dispatch

Dispatched to a ChatGPT session, same protocol as M1.5 / M1.6:
- clean tarball via `git clone --no-hardlinks` + orphan squash, **no `tar --exclude` basename globs**;
- plan + dispatch prompt with check-self-fix-escalate preconditions, degrade-and-log
  fallbacks, budget-exhaustion protocol, stop criteria pointing at load-bearing signals
  (kernel change / M0.5 red / a gate predicate that can't hold without loosening / external
  identity needing a direct grant / 致残对照 not going red);
- `BASE=$(git rev-parse HEAD)` captured before Task 1; no hardcoded SHA;
- reviewer fetches, re-runs all of §7 independently, re-does the 致残对照, merges, tags
  `m1.7-done-integrity-qualification`, bumps §13.
