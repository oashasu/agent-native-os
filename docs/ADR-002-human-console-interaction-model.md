# ADR-002 — Human Console 交互模型

日期：2026-08-29
状态：**已定向**（项目所有者基于可操作原型确认）
关联：ADR-012（Neovim/TUI 首期人机控制台）、`M1-DESIGN.md` §5.2 / §6 / §11 / §13、需求基线 G8 / G9 / G10
原型：`https://claude.ai/code/artifact/08ee31d9-037b-4e4a-b419-72ca0cfa2b9b`（可操作，全中文；三个真实工作上下文，分属不同仓库/分支）

---

## 决定

人机控制台是**一个工作上下文（WorkContext），两个镜头**：

- **IDE 工作台** —— 仿 IntelliJ IDEA：左侧目录树、多标签编辑器、底部工具窗（终端 / Git / Problems）、状态栏。
  人在这里直接改代码。AI 的 CLI 集成活在这个**终端**里。
- **Agent 工作台** —— 全屏会话：完整转录（prompt + agent 推理 + 每次工具调用及输出）、本次改动的
  **多文件清单**、工作流阶段、完成闸、回复输入行。

两个工作台**共享"当前工作上下文"这一份状态**：

| 动作 | 效果 |
|---|---|
| 切镜头（`F4` / `⌃1` / `⌃2`） | 看同一份工作的另一面，不改上下文 |
| 切上下文（顶栏切换器 / `⌃K`，**两个工作台都能操作**） | 换一份工作，**两个镜头一起换根**：目录树、分支、Git 历史、改动清单、会话转录全部切换 |

两个工作台全屏、互斥、瞬间切换、状态保留。

## 为什么

需求里 AgentSession 是"非常重要的一步"。之前的方案（含 Cursor）把它压成 IDE 的一个侧栏。
所有者明确：Agent 会话要有自己的完整工作台，IDE 也要"该有的都有"，两者平级。
当前痛点是"IDE 和跑 agent 的终端是两个独立工作台，来回切换有摩擦"——本模型用
"一个上下文两个镜头 + 瞬间切换 + 共享脊柱"来消除它。

## 架构含义（决定 M1 是否需要改）

| 关注点 | 结论 |
|---|---|
| **WorkContext 是脊柱** | Task / Workspace / AgentRun / Artifact / ToolRun / Review / SessionRecord 全部按 `work_context_id` 键控，都是同一个 WorkContext 的投影。`M1-DESIGN.md` §6（D7 瘦身 + `vibe task show` = 读投影）**已经是这个形状 —— 无需改契约**。 |
| **worktree 是真实本地目录** | `workspace.allocate` 产出磁盘上真实 git worktree。IDE 工作台**直接在这个路径上操作文件系统和本地 `git`**——目录树、编辑、提交历史都不需要 kernel 契约。kernel 只经 `workspace.get`（拿路径）+ 投影查询提供围绕它的工作上下文元数据。**这是返工风险低的根本原因。** |
| **changeset（多文件改动）** | = diff Artifact 的 `summary.files[]`。两个镜头都渲染它。M1 已有。 |
| **人手动编辑 worktree** | 与 agent 编辑等价：diff 变更 → `EvidenceRef.invalidated_at` 置位 → 旧 Review 失效（§4.3）→ 必须重跑。机制 M1 已在。 |
| **上下文枚举** | 切换器要"列出所有活动工作上下文 + 状态"。当前只有 `work.get(task_id)`。**新增 `work.query@1`**，随 M1.6 落地；M1.9 qualification（§10，单任务）不依赖它。 |
| **Agent 交互式追问**（回复行真的发消息给运行中的 agent） | M1 `agent.run@1` 是一次性 streaming。追加消息需新契约 → **M2**。M1 的回复行只演示形态。 |
| **结构化转录渲染**（工具调用折叠卡片） | 依赖 provider adapter 把原始转录归一化。M1 只保证 `raw_session_ref` blob + `agent.frame` 文本流 → 结构化是 **M1.8+ / adapter 层 / UI 阶段**。 |
| **本地 git 提交历史可视化** | UI 阶段。worktree 是本地目录，IDE 工作台直接跑 `git log` / `git show`，无需 kernel 契约。`RecoveryCheckpoint` 已有 base/head commit。 |
| **前端本体** | UI 阶段（Neovim，ADR-012）。M1 只保证读投影支持两个镜头。 |

## 不改的

`M1-DESIGN.md` 的契约面、DONE 不变量（§4）、编排模型（§7）、里程碑链 M1.7–M1.9 全部不动。
本 ADR 对 M1 的净影响：`work.query@1` 一个只读查询 + §11 的 scope 澄清 + §6/§5.2/§13 的注解。
