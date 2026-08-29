# M1.6 外派提示词 — Engineering Workflow (composition)

> 贴给 ChatGPT 聊天会话。它有 GitHub connector（经 API 提交）+ 一个**断网**执行沙箱（跑构建/测试要用附带的 tarball）。

---

## 你的任务

实现 **M1.6 — Engineering Workflow (composition)**，严格按仓库里的计划逐任务执行：

```
docs/superpowers/plans/2026-08-29-m1-6-engineering-workflow.md
```

计划共 **10 个任务**，每个任务：先写失败测试 → 跑它、确认因正确原因失败 → 写最小实现 → 跑绿 →
（行为性改动）把生产代码改动反转一次确认测试变红、再恢复 → 按计划给定的 message 提交。

**基线锚点（静态，写死）**：分支 `chatgpt/m1-6-engineering-workflow` 从 `main` 的
**`b22e0b8`** 切出。

---

## 前置条件：检查 — 自修 — 升级

每条都按这三段处理，**不要写状态断言、不要因为可自修的事停车**：

| # | 检查（给出命令） | 自修（已预授权，直接做） | 升级（只有这种才停） |
|---|---|---|---|
| 1 | `git rev-parse HEAD` 是否为 `b22e0b8` | 不是就 `git fetch origin && git checkout b22e0b8` | 该 commit 在远端不存在 |
| 2 | `git status --porcelain` 是否干净 | 有解压残留（`.DS_Store`、`vibe`、`.bin/`）直接删 | 有你没写过的**源码**改动 |
| 3 | tarball 解压后 git 可用 | ownership 报错就 `git config --global --add safe.directory <path>` | git 本身不可用 |
| 4 | `go build ./plugins/... ./cli/...` + `(cd kernel && go build ./...)` 三模块能过 | `rm -f ./vibe` 之类的产物残留 | 基线就编译失败 |
| 5 | `python3 -c "import jsonschema"` 可用 | 沙箱内 `pip install jsonschema`（若可用） | 装不上 → 契约检查降级见下 |
| 6 | 基线数字：`python3 scripts/check-contracts.py --root contracts` → `29 contracts`；`ls plugins/manifests/*.manifest.json \| wc -l` → `9` | 实际值不同就**以命令输出为准**，记录，继续 | — |

**第 6 条是纪律**：本提示词和计划里的每个数字，都必须能用这里给出的命令原样复现。
口径不一致时，**信命令，改断言，记一笔** —— 不要把它当成新发现，也不要停车。

---

## 退路（预先写明，照做，不要停车等裁决）

**原则：边角情形一律"降级 + 记录 + 继续"。停车权只留给会动摇结论的问题（见下一节）。**

| 情形 | 降级动作 |
|---|---|
| 契约数不是 31 / composition manifest 数不是 10 | 以 `check-arch.sh` 实际输出为准，改断言，记录 |
| `jsonschema` 装不上 | 跳过 `check-contracts.py`，改为人工核对 `contracts/catalog.json` 与目录一致，记录 |
| `check_composition.py` 的 FIX-4 豁免规则与真实 manifest 对不上 | 按 Task 1 的意图调规则（composition 免 fan-in 上限、但禁止 stateful export），不改 manifest 去迁就脚本，记录 |
| smoke 的 WAITING_REVIEW 轮询超时/抖动 | 加大轮询上限与退避（不要改成 sleep 固定值），记录；连续 3 次仍失败才算触及承重面 |
| policy 重构后某个 fragment smoke 报 DENIED | 把那一条"执行类"调用切到 `-identity m1-dev -token $DEV_TOKEN`（计划 Task 8 Step 3 的既定手法），记录 |
| workflow 插件的 fence lease / data_namespace 配置不对 | 按其它 stateful 插件（如 `session`）的 manifest 形状对齐，记录 |
| 计划里的命令/脚本有笔误（如 `set -o pipefail` 下预期非零命令进管道） | 直接修正，**保持原意不变**，记录为偏离 |
| 某个数字/计数与计划字面不符 | 信命令，改断言，记录 |

---

## 停车判据（指向信号，不指向位置）

只有触及**承重面**才停下并报告 —— 即下列任一信号出现：

- 需要修改 `kernel/internal`、`kernel/cmd`、`kernel/sdk` 才能推进（**G1**）。
- kernel M0.5 qualification 变红。
- DONE gate（计划 Task 3 的 §4.3 合取式）无法在不放宽判据的前提下实现。
- 需要给外部身份直接授予 `work.transition@1` 才能跑通（这会摧毁 §4.2 的不可绕过性）。
- 需要引入 `service_authority` 或新的外部 Go 依赖。
- 致残对照**不变红** —— 说明测试在空转，这比"没写完"严重。

**不构成停车理由**：计数不符、脚本笔误、smoke 抖动、轮询超时、manifest 形状微调、
`safe.directory`、缺 `jsonschema`。这些全部按上表降级。

---

## 预算耗尽协议（M1.5 的教训，务必照做）

M1.5 那次在 Task 8 中途耗尽工具交互额度，留下"本地已完成但 connector 未提交"的半吊子状态。
这次**预先约定**：

- 感到额度将尽时，**在任务边界停**，不要停在任务中间。
- Task 6 结束是天然的干净分界（插件本体完整、可独立测试）；Task 7–10 是集成/policy/smoke 尾巴。
- 停之前必须完成三件事：
  1. 把**已完成任务**的 connector commit 全部推上去（不要留"本地有、远端没有"）；
  2. 推进分支 ref；
  3. 报告里明确写出：**最后一个已推送的 commit SHA** + **下一个未开始的任务号**。
- **不要**为了"看起来完成"而跳过验收或伪造输出。没跑就说没跑。
- 尾巴由我在本地补完 —— 这是既定预案，不是失败。

---

## 硬约束

- **不要碰 `docs/M1-DESIGN.md`** —— 不编辑、不 stage、不提交。§13 由我合并后更新。
  （connector 上传该大文件会损坏 blob，这是已知限制。）
- **不改 `kernel/` 源码。**
- **不新增外部 Go 依赖**（沙箱无 module proxy）。
- 模块路径：kernel `github.com/example/agent-native-microkernel`；plugins
  `github.com/example/agent-native-os/plugins`；CLI `github.com/example/agent-native-os/cli`。
- 每条 commit message 结尾恰好是：
  ```
  Co-Authored-By: ChatGPT <chatgpt@users.noreply.github.com>
  Plan: docs/superpowers/plans/2026-08-29-m1-6-engineering-workflow.md
  ```
- commit 作者写 `ada <oashasu@gmail.com>`（connector 可能替换成它自己的认证身份 —— 已知限制，不算偏离）。

---

## 验收（Task 10）与 PR

按计划 Task 10 逐步执行，**把命令的原始输出贴进 PR 描述**，不要转述：

1. 三模块 build → exit 0
2. `go test ./plugins/... ./plugins/_template ./cli/...` + `(cd kernel && go test ./...)` → 全 `ok`
3. `cd kernel && ./scripts/build.sh >/dev/null && python3 tests/integration/m05_qualification.py 2>&1 | tail -2` → `PASSED`
4. `bash scripts/check-arch.sh` → 四行全过（数字以实际输出为准）
5. `bash scripts/smoke.sh` **跑 5 次**，每次都要 `M1.6 WORKFLOW SMOKE: OK` + `M1 SMOKE: PASSED`，零 `FAIL`
6. G1 两个锚点均为空：
   ```
   git diff --name-only b22e0b8 HEAD -- kernel/internal kernel/cmd kernel/sdk
   git diff --name-only b22e0b8 HEAD -- docs/M1-DESIGN.md
   ```
7. 开 PR：`chatgpt/m1-6-engineering-workflow` → `main`，标题
   **M1.6 — Engineering Workflow (composition)**，正文含 10 个任务的 commit 表、
   上述逐条原始输出、以及**所有降级/偏离的清单**。

**致残对照要写进 PR**：每个行为性任务说明你反转了什么、哪条测试因什么消息变红、恢复后是否绿。
我会独立复跑并自己再做一次致残对照 —— 自报不构成验收。

---

## 交付物

- 分支 `chatgpt/m1-6-engineering-workflow`（10 个 commit）
- PR **M1.6 — Engineering Workflow (composition)**
- 报告：完成到哪个任务、最后推送的 commit SHA、降级清单、致残对照结果

附带 tarball 是含 `.git` 的完整仓库快照（沙箱断网，构建/测试都在里面跑）。
connector 用于提交；两边 SHA 不同是正常的。
