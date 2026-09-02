# M1.8 外派提示词 — Real Provider Adapter (codex)

> 贴给负责实现的 ChatGPT 会话。会话有 GitHub connector（经 API 提交 + 开 PR）+ 一个**断网**执行沙箱。构建/测试都在附带的 tarball 工作树里跑。tarball 是权威基线；connector 侧 SHA 与本地不同是正常现象。

---

## 你的任务

严格按仓库里的 spec + implementation plan 实现 **M1.8 — Real Provider Adapter (codex)**：

```
docs/superpowers/specs/2026-09-01-m1-8-real-provider-adapter-design.md
docs/superpowers/plans/2026-09-01-m1-8-real-provider-adapter.md
```

计划共 **11 个任务**。Task 1–10 各按 TDD（先写失败测试 → 跑它、确认因正确原因失败 → 写最小实现 → 跑绿 → 行为性改动做一次致残对照 → 提交）执行，每个任务一个 connector commit；Task 11 只做最终验收 + 完整致残 sweep + 开 PR。

**目标**：给 `org.vibe.agent.harness` 加 `RealProvider`（子进程驱动的 `Provider`），`codex` 为真实 provider #1，启动时运行时发现、按 run 可选；把 `agent.run.cancel` 从"只改 Store 状态"升级为"真正停掉正在运行的 provider"。**mock 全程保持默认**，`smoke.sh` / `qualify-done-integrity.sh` / `check-arch.sh` 不改。

**你不会跑真实 codex**（沙箱断网）——只对着 `.bin/fake-agent-cli` 这个确定性 fixture 构建和测试。真实 codex 的端到端验证是 reviewer 的事（`scripts/verify-real-provider.sh`，你写它，但它在没有 `VIBE_REAL_PROVIDER=codex` 时会 SKIP，你只需验证 SKIP 分支）。

---

## 开工前：BASE + 工具链

tarball 解压后立即：

```bash
git log --oneline            # 只应有一条 "M1.8 handoff snapshot of agent-native-os"
git status --porcelain       # 应为空
go version                   # 期望 go1.19.x —— 见下方硬约束
BASE="$(git rev-parse HEAD)"; export BASE
git switch -c "chatgpt/m1-8-real-provider-adapter"
```

**BASE 必须动态捕获，禁止写死 SHA。** 之后所有 G1 检查用 `"$BASE"`。当前分支应是干净的单提交 handoff snapshot；不要从 connector 侧历史补文件。

基线检查：

```bash
go build "./plugins/..." "./cli/..." && ( cd "kernel" && go build "./..." )
rm -f "./vibe"
bash "scripts/build.sh" >/dev/null && echo BUILD_OK
python3 "scripts/check-contracts.py" --root "contracts"        # 期望 31 contracts
```

基线编译失败、`git status --porcelain` 有非残留改动、或 HEAD 不是 tarball 快照 —— 停止并报告。

---

## 前置处理协议：检查 — 自修 — 升级

边角问题按此顺序处理，**不要因为可恢复问题停车**：

1. `safe.directory` / `.DS_Store` 残留：直接 `git config --global --add safe.directory <path>` 或删除。
2. 计划里的数字/计数与实际不符：**信命令输出**，改断言、记录，不改生产文件迁就数字。契约数应始终 31、manifest 10。
3. 计划命令的 shell 笔误（如 `set -o pipefail` 下预期非零命令进管道、路径未加引号）：保持原意直接修正，记为偏离。
4. `jsonschema` 或其它可选工具缺失：按计划降级并记录，不伪造通过。
5. 某个 fixture 测试在慢沙箱抖动（进程未及时启动 / pid 文件未及时出现）：加大轮询上限与超时（**不要改成固定 sleep**），记录；连续 3 次仍失败才算触及承重面。
6. `go vet` 报新加的 package 级函数"未使用"：Task 2 的平台函数在 Task 3 才被用；Go 不会因此编译失败，继续到 Task 3 一起提交即可。**不要**加占位引用去消警告。

**致残对照用 `apply_patch` 手工恢复被改的那一处**，不要用 `git checkout` / `git restore`——那会把同一任务里刚写好、尚未提交的实现一起冲掉。改前先记下原始 hunk。

---

## 停车判据（指向信号，不指向位置）

只有触及**承重面**才停下并报告——即下列任一：

- 需要修改 `kernel/` 任何路径（G1）。
- kernel M0.5 qualification 变红。
- 某条**设计不变量**（spec §2：provider 中立 / mock 默认 / 未知 provider 在 `RecordStarted` 前拒 / `RealProvider` 不解析 codex 事件 / `provider_metadata` 只含 `provider`+`exit_code` / 取消先停 provider 再落终态）**无法在不违反的前提下满足**。
- 需要改 `agent.run@1` 或 stream 契约的**形状**（加可选 request 字段不算）。
- 需要引入 Go 1.20+ API 或新的外部 Go 依赖。
- 需要放宽 policy / bindings，或改 `smoke.sh` / `qualify-done-integrity.sh` / `check-arch.sh`。
- 致残对照**不变红**——说明测试在空转，这比"没写完"严重。

**不构成停车理由**：计数不符、脚本笔误、fixture 抖动、`go vet` 未使用警告、`safe.directory`、缺 `jsonschema`、非 unix 交叉编译细节。

---

## 硬约束

- **工具链 Go 1.19。** `plugins/go.mod` / `cli/go.mod` 声明 `go 1.19`，沙箱大概率也是。**不得使用 Go 1.20+ API** —— 尤其 `exec.Cmd.Cancel`、`exec.Cmd.WaitDelay`、`errors.Join`、`context.WithoutCancel`。`RealProvider` 用 `os.Pipe` 写端 + 手动 `Start`/`Wait`/`select ctx.Done`/`killProcessGroup`（计划 Task 3 给了完整代码）。若 `go version` 显示 ≥ 1.20，按计划代码照写即可（向下兼容），但**不要**依赖 1.20+ 符号。
- **不碰 `kernel/`。**
- **不碰 `docs/M1-DESIGN.md`** —— 不编辑、不 stage、不提交。§6/§8/§13 由 reviewer 合并后调和。
- **不改 `config/m1-{policy,bindings}.json`、`plugins/manifests/*`、`scripts/{smoke,qualify-done-integrity,check-arch}.sh`。**
- **不新增外部 Go 依赖**（沙箱无 module proxy）。
- 模块路径：kernel `github.com/example/agent-native-microkernel`；plugins `github.com/example/agent-native-os/plugins`；CLI `github.com/example/agent-native-os/cli`。
- **Shell 命令里的路径用双引号包裹**（计划里已如此写）。
- **每条 commit：**
  - subject 用格式 `[中文模块][英文类型][中文摘要]`，类型 ∈ `add|fix|refactor|chore`（计划里每个 Task 的 commit 块已给出确切 subject 和中文正文，照抄）。
  - message 结尾恰好是：
    ```
    Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
    Plan: docs/superpowers/plans/2026-09-01-m1-8-real-provider-adapter.md
    ```
  - 作者 `ada <oashasu@gmail.com>`（connector 可能替换成它自己的认证身份——已知限制，不算偏离）。

---

## 允许出现在最终 diff 里的文件（白名单，22 个）

```
cli/vibe/main.go
contracts/workflow.engineering.run/v1/schema.json
plugins/agent-harness/discovery.go
plugins/agent-harness/discovery_test.go
plugins/agent-harness/fakeagentcli/main.go
plugins/agent-harness/handlers.go
plugins/agent-harness/handlers_test.go
plugins/agent-harness/main.go
plugins/agent-harness/real_provider.go
plugins/agent-harness/real_provider_exec_other.go
plugins/agent-harness/real_provider_exec_unix.go
plugins/agent-harness/real_provider_test.go
plugins/agent-harness/runreg.go
plugins/agent-harness/runreg_test.go
plugins/agent-harness/session.go
plugins/agent-harness/session_test.go
plugins/engineering-workflow/handlers.go
plugins/engineering-workflow/handlers_test.go
plugins/engineering-workflow/pipeline.go
plugins/engineering-workflow/pipeline_test.go
scripts/build.sh
scripts/verify-real-provider.sh
```

`plugins/agent-harness/provider_test.go` 的 race 修复已在 `$BASE` 里，**不应**再出现在 diff。`kernel/`、`docs/M1-DESIGN.md` 必须为空。

---

## 预算耗尽协议

- 只在 **Task 边界**停，不停在半个任务中间。
- 停之前：把已完成任务的 connector commit 全部推上去、推进分支 ref、报告**最后一个已推送的 commit SHA + 下一个未开始的任务号**。
- 没跑过的测试/验收明确写"未运行"，禁止伪造输出。
- Task 11 之前不能把未完成状态描述成 M1.8 完成。
- 尾巴由 reviewer 本地补完——既定预案，不是失败。

---

## 最终验收（Task 11）与 PR

按计划 Task 11 逐步执行，**把命令原始输出贴进 PR**，不要转述：

1. 三模块 build（含 `.bin/fake-agent-cli`）→ exit 0
2. `go test` plugins + `_template` + cli + kernel 全 `ok`（含 `real_provider` / `discovery` / `runreg` / live-cancel / engineering-workflow provider 测试）；`go test -race ./plugins/agent-harness/` 无 race
3. kernel `m05_qualification.py` → `PASSED`
4. `check-arch.sh` → `31 contracts` / `10 manifests` / `ARCH CHECKS OK`（未改动）
5. DONE-integrity qualification `qualify-done-integrity.sh` ×3 → `DONE-INTEGRITY QUALIFICATION: OK`（provider 改动不得回归它）
6. `smoke.sh` ×5 → `M1 SMOKE: PASSED`、0 `FAIL`、0 orphan（mock 仍默认）
7. `scripts/verify-real-provider.sh` → `SKIP…`、exit 0
8. **完整致残 sweep（计划 Task 11 Step 8 的 M1–M11 表）**：每条先变红、`apply_patch` 恢复后变绿，记录精确红文案
9. G1 + 白名单：`git diff --name-only "$BASE" HEAD -- "kernel"` 为空；`git diff --name-only "$BASE" HEAD` 恰好是上面 22 个文件
10. 开 PR：`chatgpt/m1-8-real-provider-adapter` → `main`，标题 **M1.8 — Real Provider Adapter (codex)**，正文含 10 个任务的 commit 表、上述逐条原始输出、所有降级/偏离清单、致残 sweep 结果，以及"reviewer 会独立重跑并自己再做一次致残 + 本地跑 `verify-real-provider.sh`"的说明。**自报不构成验收。**

---

## 交付物

- 分支 `chatgpt/m1-8-real-provider-adapter`（10 个 commit）
- PR **M1.8 — Real Provider Adapter (codex)**
- 报告：完成到哪个任务、最后推送的 commit SHA、降级清单、致残结果

附带 tarball 是含 `.git` 的完整仓库快照（沙箱断网）。connector 用于提交；两边 SHA 不同是正常的。
