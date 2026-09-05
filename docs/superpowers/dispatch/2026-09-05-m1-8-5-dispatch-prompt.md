# M1.8.5 外派提示词 — workspace.get{work_context_id} finds released workspaces

> 贴给负责实现的 ChatGPT 会话。会话有 GitHub connector（经 API 提交 + 开 PR）+ 一个断网执行沙箱。构建/测试都在附带的 tarball 工作树里跑。tarball 是权威基线；connector 侧 SHA 与本地不同是正常现象。

---

## 你的任务

严格按仓库里的 spec + implementation plan 实现 **M1.8.5 — workspace.get{work_context_id} finds released workspaces**：

```
docs/superpowers/specs/2026-09-04-m1-8-5-workspace-by-context-recovery-design.md
docs/superpowers/plans/2026-09-04-m1-8-5-workspace-by-context-recovery.md
```

计划共 **5 个任务**（Task 1–4 各按 TDD 执行，每任务一个 connector commit；Task 5 只做最终验收 + 致残 sweep + 精确白名单核对）。

**目标**：`workspace.get{work_context_id}` 现在只返回 `Status==ALLOCATED` 的 workspace；一旦 `workspace.release` 过就查不到了，只能靠 `workspace_id` 查。这次要让它按确定性规则（ALLOCATED 优先 → `AllocatedAt` 降序 → `ReleasedAt` 降序 → `ID` 升序）也返回已用 `preserve` 策略释放的 workspace，让一个只有 `task_id`/`work_context_id` 的冷启动调用方（未来的 Console）也能找回一单已完成任务的工作目录。

**这次不需要真实外部依赖**：单一插件、无凭证、无网络、测试全程确定性。选择规则已经在 spec/plan 里锁死——**你只做 TDD 实现，不重新做架构决策**。

---

## 开工前：BASE + 工具链

```bash
go version                   # 期望 go1.19.x
git log --oneline            # 只应有一条 "M1.8.5 handoff snapshot of agent-native-os"
git status --porcelain       # 应为空
git switch -c "chatgpt/m1-8-5-workspace-by-context-recovery"
```

然后严格按计划文档的 **"Before Task 1: capture `BASE`"** 一节执行——把 `git rev-parse HEAD` 的结果写进 `/tmp/m185-base.txt`（不是裸环境变量：本次会话里不同命令可能落在不同 shell），确认只有一行 40 字符的 SHA。这一步之后才开始 Task 1。

基线检查：
```bash
go build "./plugins/..." "./cli/..." && ( cd "kernel" && go build "./..." )
rm -f "./vibe"
bash "scripts/build.sh" >/dev/null && echo BUILD_OK
python3 "scripts/check-contracts.py" --root "contracts"        # 期望 31 contracts
```

基线编译失败、`git status --porcelain` 有非残留改动、或 HEAD 不是 tarball 快照——停止并报告。

---

## 前置处理协议：检查 — 自修 — 升级

边角问题按此顺序处理，**不要因为可恢复问题停车**：

1. `safe.directory` / `.DS_Store` 残留：直接 `git config --global --add safe.directory <path>` 或删除。
2. 计划里的数字/计数与实际不符：**信命令输出**，改断言、记录，不改生产文件迁就数字。契约数应始终 31。
3. 计划命令的 shell 笔误（路径未加引号、`set -o pipefail` 下预期非零命令进管道）：保持原意直接修正，记为偏离。
4. 某条命令在慢沙箱抖动（进程未及时启动 / kernel 未及时 query-ready）：加大轮询上限，**不要改成固定 sleep**，记录；连续 3 次仍失败才算触及承重面。

**致残对照用手工编辑 + 手工精确撤销那一处**（计划里每条都写了原文和撤销方式），不要用 `git checkout` / `git restore`——那会连同一任务里刚写好、尚未提交的实现一起冲掉。

---

## 停车判据（指向信号，不指向位置）

只有触及**承重面**才停下并报告——即下列任一：

- 需要修改 `kernel/` 任何路径。
- 需要修改 `plugins/workspace/` 之外的任何插件、或 `config/m1-{policy,bindings}.json`、或任何契约的 `request`/`response`/`version`/`kind`（本次只允许在 `contracts/workspace.get/v1/schema.json` 加一个顶层 `description` 字符串）。
- **`GetActiveByContext` 的行为或其既有测试需要改动**——它必须原样保留。
- **选择规则需要偏离** spec/plan 锁死的顺序（ALLOCATED 优先 → `AllocatedAt` 降序 → `ReleasedAt` 降序 → `ID` 升序，`delete` 策略永不作候选，无候选 → `NOT_FOUND`，不引入 `CONFLICT`）。
- 致残对照**不变红**——尤其注意计划里明确标注的：M2 的致残只能用 `TestGetByContextPrefersAllocatedOverNewerPreserveReleased` 见证（不是 `TestGetByContextPrefersLatestAllocated`），M3 的致残只能用 `TestRefLessOrdering` 见证（不是 `GetByContext` 级别的测试，那个在这个致残下依赖 map 遍历顺序，不保证变红）。按计划写的用，不要自己换见证测试。
- `bash scripts/check-arch.sh` 契约数偏离 31、或 manifest 数偏离 10。

**不构成停车理由**：计数不符、脚本笔误、fixture 抖动、`safe.directory`、非 unix 交叉编译细节。

---

## 硬约束

- **工具链 Go 1.19。** 不引入新的外部 Go 依赖。
- **不碰 `kernel/`。不碰 `docs/M1-DESIGN.md`**（不编辑、不 stage、不提交——connector 会损坏大文本上传；这份文档由 reviewer 合并后统一调和）。
- **不改 `config/m1-{policy,bindings}.json`、`plugins/manifests/*`、`scripts/{smoke,check-arch,qualify-done-integrity}.sh`**（`scripts/smoke-workspace.sh` 除外——这是 Task 4 明确要改的那一个 smoke 片段）。
- 模块路径：`plugins` 是 `github.com/example/agent-native-os/plugins`。
- **Shell 命令里的路径用双引号包裹**（计划里已如此写，照抄）。
- **每条 commit：**
  - subject 用格式 `[中文模块][英文类型][中文摘要]`，类型 ∈ `add|fix|refactor|chore`（计划里每个 Task 的 commit 块已给出确切 subject 和中文正文，照抄）。
  - message 结尾恰好是：
    ```
    Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
    Plan: docs/superpowers/plans/2026-09-04-m1-8-5-workspace-by-context-recovery.md
    ```
  - 作者 `ada <oashasu@gmail.com>`（connector 可能替换成它自己的认证身份——已知限制，不算偏离）。

---

## 允许出现在最终 diff 里的文件（白名单，6 个）

```
contracts/workspace.get/v1/schema.json
plugins/workspace/handlers.go
plugins/workspace/handlers_test.go
plugins/workspace/store.go
plugins/workspace/store_test.go
scripts/smoke-workspace.sh
```

`kernel/`、`docs/M1-DESIGN.md`、任何其他插件必须为空。

---

## 预算耗尽协议

- 只在 **Task 边界**停，不停在半个任务中间。
- 停之前：把已完成任务的 connector commit 全部推上去、推进分支 ref、报告**最后一个已推送的 commit SHA + 下一个未开始的任务号**。
- 没跑过的测试/验收明确写"未运行"，禁止伪造输出。
- Task 5 之前不能把未完成状态描述成 M1.8.5 完成。

---

## 最终验收（Task 5）与 PR

按计划 Task 5 逐步执行，**把命令原始输出贴进 PR**，不要转述：

1. 全量 `go test` + `-race` → `GO_TESTS_OK`、`RACE_OK`
2. `check-arch.sh` → `31 contracts` / `10 manifests` / `ARCH CHECKS OK`（未改动）
3. `smoke.sh` ×5 → 每次都要看到 `M1 SMOKE: PASSED` **和** `M1.8.5 WORKSPACE-BY-CONTEXT-RECOVERY SMOKE: OK`，0 `FAIL`，0 orphan
4. **独立重跑 Task 1/2 的四条致残（M1–M4）**：每条先变红（附精确报错文案）、手工撤销后变绿
5. 精确白名单核对：`git diff --name-only "$(cat "/tmp/m185-base.txt")" HEAD` 恰好是上面 6 个文件

全绿后：开 PR，`chatgpt/m1-8-5-workspace-by-context-recovery` → `main`，标题 **M1.8.5 — workspace.get{work_context_id} finds released workspaces**，正文含 4 个任务的 commit 表、上述逐条原始输出、致残 sweep 结果（每条：mutation、命令、精确红文案、绿）、降级/偏离清单，以及"reviewer 会独立重跑并自己再做一次致残"的说明。**自报不构成验收。**

---

## 交付物

- 分支 `chatgpt/m1-8-5-workspace-by-context-recovery`（4 个 commit）
- PR **M1.8.5 — workspace.get{work_context_id} finds released workspaces**
- 报告：完成到哪个任务、最后推送的 commit SHA、降级清单、致残结果

附带 tarball 是含 `.git` 的完整仓库快照（沙箱断网）。connector 用于提交；两边 SHA 不同是正常的。
