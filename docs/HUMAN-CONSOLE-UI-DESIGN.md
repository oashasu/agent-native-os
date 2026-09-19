# Human Console UI 设计 — 基于 VSCode 主仓库二开的轻量 IDE + Agent 会话工作台

> 实现的对象：`docs/ADR-002-human-console-interaction-model.md` 的"一个 WorkContext，两个镜头"。
> 载体：**基于 VSCode 官方主仓库（microsoft/vscode）二次开发**（[ADR-004](./ADR-004-vscode-fork-ui-carrier.md)，重开并取代 ADR-003/012 的 TUI 首选）。交互模型与键盘映射按"语义等同、可再映射"约束书写。
> 状态：**UI 设计基线草稿 v0.2（载体已定向 VSCode 二开，待实现验证）**。确认后生效为前端实现基线，并作为 ADR-004（VSCode 主仓库二开）的落地依据。
> 前置数据契约：M1 的七个读投影（`work.get / workspace.get / agent.run.query / artifact.query / tool.run.query / review.query / session.query`）+ 本地 worktree 与 git。**M1.9 的 Console 读投影充分性验收失败 = 本设计的两个镜头拿不到数据 = 设计不成立**（与 ADR-002 §怎么才算这个决定错了 同一条机器判据）。
> **可点击原型 v2**：`prototypes/human-console.html`（单文件、零依赖，浏览器直接打开）。覆盖本文档全部核心交互：`⌃1/⌃2/F4` 切镜头、`⌃K` 上下文换根、**`⌃S` 会话导航器 / `⌃W` 工作空间导航器**、Agent 镜头 = **DSH Harness 同构的「工作区 / 会话」导航树 + 对话流水**（`[`/`]` 循环、点击导航树切换）、**IDEA 风格层级目录树（折叠/展开 + j/k/h/l 键盘导航）**、改动 chips → 全宽 diff 审阅、`#t23-agent-run-6`/`#agent`/`#t31` 等 hash 直达。

---

## 1. 设计命题

> 一个开发者在 **30+ 个会话、多个 Worktree、多个并行 Agent** 的真实工作场景里，只靠键盘，在"改代码（IDE 镜头）"与"盯/指挥 Agent（Agent 镜头）"之间**零摩擦瞬间切换**；无论切多少次，**上下文（任务、分支、改动、转录、闸）永不丢失、永不重复导航**。

- 用户体验目标只有一句：**"我始终在同一份工作里，只是看正面还是背面。"**
- 轻量目标：整个 Human Console 是 VSCode 官方主仓库的一个 fork + 一组镜头/视图与桥接插件；编辑器、树、diff、LSP、调试、终端、git **全部 REUSE 自 VSCode**（v0.4 §14 REUSE NOW）。对比目标「替代重型 IDEA」（IntelliJ ~2GB 内存/分钟级启动），fork 版 Electron ~300–600MB/秒级启动，仍是轻量级。

## 2. 信息架构总图

```text
                     Work Context 脊柱（唯一状态源，长存于平台）
   task · worktree/branch · changeset · AgentRun · Evidence · Review · SessionRecord
                                    │
        ┌───────────────┬───────────┴───────────┬───────────────┐
        ▼               ▼                       ▼               ▼
   IDE 镜头（正面）  Agent 镜头（背面）        ⌃K 上下文切换器   命令面 :Vibe…
   改代码/读代码      看/指挥 Agent             （两个镜头都能开）  （CLI 透传）
   全屏 · 状态保留     全屏 · 状态保留          换根：两镜头一起换    无私有状态
```

不变量（对齐 P13/P15 与 ADR-002）：
1. 任何 UI 元素**不拥有** canonical 状态；状态只来自读投影 / 本地 git / 本地文件系统。
2. 关闭镜头 = 只 detach 视图；Terminate/Archive/Delete 是不同的显式操作（UX-SES-001/002）。
3. 两个镜头永远**全屏互斥**，切换是"换一组布局"而不是"开一个新页签"。

## 3. IDE 镜头

> 形态基线（原型 v7，2026-09-19）——**IDE 镜头按 IntelliJ 实录 + VSCode 观感双重校准**：`~/IdeaProjects/workspace-merge` 实拍（2682×1964）提供内容锚点（面包屑、Outline 成员树、CodeLens 行内作者批注、底部 Terminal）；观感取 VSCode 原生三分栏：左侧活动栏（资源管理器/搜索/源代码管理/运行/扩展）+ 侧边树 + 编辑区，UI 文本用比例字体、等宽仅限代码域（v7 修正 v2–v6 全局等宽导致的"Vim 观感"）。两镜头视觉边界：IDE=浅底三分栏，Agent=Harness 暖深蓝气泡流。（正面：人改代码）

```text
┌ tabline［脊柱，两镜头共享同一渲染］──────────────────────────────────────┐
│ T17·PaymentService溢出修复 │ T17-impl ●dirty │ IN_REVIEW │ codex │ 2m │ !0 │
├─目录树────────────┬─编辑区 tabs──────────────────────────────────────────┤
│ ☰ src/main/java   │  PaymentController | TradeTransfer | +T17Test       │
│   M Calculator    │  ┌──────────────────────────────────────────────┐   │
│   M CalculatorT   │  │ 编辑器（VSCode 编辑器 + LSP 高亮）              │   │
│  …                │  │ agent 本次改动块：行号前缀 ▍+ 微亮底色         │   │
│  j/k 导航          │  └──────────────────────────────────────────────┘   │
│  Enter 打开        │   VSCode 原生窗口/键位保留                          │
├─底部工具窗───────────────────────────────────────────────────────────────┤
│ [Term│Git│Problems│Tests]                                               │
│  Term:  与该上下文 cwd 绑定的 agent CLI / shell（独立进程，关窗不死）     │
│  Git:   git diff/blame/log（本地 repo 直连，非 kernel 契约）             │
│  Tests: 最近 tool.run 结果（tool.run.query 投影）                        │
├─状态栏───────────────────────────────────────────────────────────────────┤
│ workspace.path │ branch T17-impl │ +12/-3 │ LSP ✓ │ <C-1>IDE <C-2>Agent  │
└──────────────────────────────────────────────────────────────────────────┘
```

设计要点：
- **目录树 = 层级 Project 树（IDEA Project 窗口语义）**：worktree 真实目录递归映射为**可折叠目录**（`▸/▾`、缩进导线 `├─/└─/│`、任意深度），文件带 git 状态装饰（M/A/+/U 标色），数据源是磁盘与本地 git，不经 kernel。键盘导航与 IDEA Project 窗口一致：`j/k` 移动、`Enter` 展开目录/打开文件、`h/←` 折叠或上跳父目录、`l/→` 展开；单击文件即开 tab（双击语义=展开目录）。目录行折叠后**行本身保留**（只隐藏子级），换根后目录展开状态随上下文保留。
- **编辑区 tabs = 该上下文最近编辑文件**（LRU），切上下文时整组替换。
- **agent 改动可视化**：对 `Artifact.summary.files[]` 中每个文件，把 worktree 当前内容与 `base_commit` 的 diff 直接渲染为编辑器内的高亮块（行号前缀），人一眼看出"这次 Agent 动了哪几行"。
- **Term 页签**：从 `workspace.get.path` 直接 `cd` 进去；关闭终端页签 ≠ 终止任何进程（进程归 shell/agent 自身管理，或对接 Runtime Supervisor）。
- 底部工具窗四页签用 `[`/`]` 或 `⌃\` 切换；`⌃\` 亦唤起浮动"命令/符号搜索"（LSP + rg，UI 层自己实现，无契约）。

## 4. Agent 镜头（背面：看与指挥 Agent）

> 形态基线（原型 v5，2026-09-19）：**Chat | Changes | Review 三段分页**（Cursor 3 参考，见 §4.1）——主区顶部为"正在做"英雄区（大标题 + 状态 + 模型徽标 + 停止），消息流为聊天质感卡片（头像/时间戳），改动树与审阅各占独立页，底部 Cursor 式 Composer 常驻。信息架构仍与 Harness 同构：左侧「工作区/会话」导航树。

```text
┌ tabline［脊柱，同 IDE 镜头］──────────────────────────────────────────────┐
├─工作区/会话导航（约 1/4）──┬─会话主区（约 3/4）────────────────────────────┤
│ 工作区                     │  run-7 · codex · ●ACTIVE · 2m   /work/t17/…  │
│ ▾ /work/t17/payment-svc    │  ▍plan   分析溢出点与替代方案                 │
│    ●run-7 ACTIVE           │  ▍tool   read Calculator.java (L12-40)       │
│    ○run-5 DETACHED         │  ▍think  采用 Math.addExact，覆盖 ± 溢出     │
│    ◑run-2 ARCHIVED         │  ▍edit   Calculator.java ▍+3 -1              │
│ ▾ /work/t23/batch-check    │  ▍tool   run mvn -q test → PASS (3.2s)       │
│    ●run-9 ACTIVE           ├─上下文条────────────────────────────────────┤
│    ○run-6 DETACHED         │  本次改动 [M Calculator.java +3/-1 │ M CalcTest +8] │
│ ▾ /work/t31/jpa-audit      │  ▶ IMPL→TEST→REVIEW ● ● ○   DONE-gate AC1✓ AC2✓  │
│   （尚无会话）              │  review PENDING · diff=art-14   (a)批准 (r)打回    │
│                            │  > 追问 run-7…（M1 演示形态；M2 真发送）           │
└────────────────────────────┴──────────────────────────────────────────────────┘
```

设计要点：
- **信息架构 = 当前 DeepSeek Harness 工作台同构**：左侧「**工作区 / 会话**」导航树（工作区 = WorkContext 的 worktree：路径 + 分支 + dirty，可折叠 ▸/▾；其下挂 run-* 会话：状态圆点 + id + provider + 时长），点击会话即整树定位（切上下文 + 选中该会话）；主区 = 当前会话的**对话流水** + 底部**上下文条**。替换掉单条"会话条"的旧设计——会话的归属与切换由"工作区→会话"两级导航承载。
- **会话与上下文两级分离**：改动 / 阶段 / 完成闸 / review 是**上下文级**（一份工作一份）；转录、provider、运行状态是**会话级**。`[`/`]` 循环当前工作区会话；`⌃S` 会话导航器列出**全部工作区的所有会话**（跨上下文扁平枚举，Enter 一步定位）。
- **对话流水 = 结构化事件流**，不是 PTY 滚动文本：每条 `agent.frame` / `tool_call` / `tool_result` / `approval` / `test` / `git` 都是一行可跳转、可折叠的卡片（kind 图标前缀）。`g` 跳到下一个 tool 卡片；点击折叠/展开推理块。数据源：`agent.run.query` + `raw_session_ref`（blob.get）+ 各事件投影——**结构化程度取决于 adapter 归一化，M1 保文本流，M1.8+ 渐次结构化**（对齐 ADR-002 架构含义表）。
- **上下文条 = 本次改动「改动树」（Cursor 式 change tracking）**：`Artifact{kind=diff}.summary.files[]` 每文件一行（M/A/D 徽标 + 增减行数）+ 逐文件 `revert` + `全部还原`（= 还原意图，落盘语义 = 本地 git/worktree 操作，与人手改动同权，ADR-002）；点击文件行 → 全宽 diff 审阅（hunk 分组，j/k 选 hunk，y/n 接受/拒绝，本地预览标记；头部带改动前快照 `ck-*` 与 Restore=回滚意图）+ 阶段条 + 完成闸 + review 行。
- **会话头部（Cursor 式 "what's happening now"）**：`run-7 · codex · ●ACTIVE` 右侧显示「正在做：任务标题的一句话摘要」+ ⏹ 停止按钮（Interrupt 语义，M2 接 agent.run 停止契约）。
- **工具审批（Cursor 式 approval）**：需要权限的工具卡（shell 执行等）挂「需要权限」footer，`(1) 允许一次 / (2) 始终允许`（`1`/`2` 键）；允许策略记录在会话事件流（M2 `agent.run` approval 契约对齐）。
- **回复输入行 = Composer**：模式/模型选择器（Agent ▾ · codex ▾）+ `@` 上下文 chips（@file / @rules / @MCP）+ `⌘Enter` 发送（M1 仅演示形态，ADR-002：追加消息需新契约 → M2）。
- **stage 条 + 完成闸**：工作流阶段（IMPL→TEST→REVIEW→DONE）与 DONE gate 合取式逐项 ✓/✗ 展示（数据= `work.get` 状态 + `EvidenceRef[]{kind,outcome}` + `review.query`），失败项红色高亮并跳到对应证据。
- **review 行**：`(a)` 批准 / `(r)` 打回 = 现有 `vibe review decide` 的键盘绑定；acceptance_results 逐条可展开看 notes 与 evidence_refs。
- **回复输入行**：M1 只演示形态（ADR-002：追加消息需新契约 → M2）。
### 4.1 Cursor 3.x 参考与采纳（原型 v4 已落地，2026-09-19）

载体锁定 VSCode 二开（ADR-004）后，Agent 镜头交互以 **Cursor（最成功的 VSCode-fork AI IDE）agent 面板为参照系**。
对照与采纳映射（原型 `prototypes/human-console.html` v4 已实现 ✅，未采纳的写明理由）：

| Cursor 交互（2.x/3.x 形态） | 本设计采纳 | 说明 / 差异 |
|---|---|---|
| Composer：模式（Ask/Plan/Agent）+ 模型选择 + `@` 上下文 | ✅ composer（Agent ▾ · codex ▾ · @file/@rules/@MCP chips · ⌘Enter 发送） | 发送目前是演示；M2 才真走 agent.run 追加消息契约 |
| 面板头部 "what's happening now" + 停止/打断 | ✅ 会话头「正在做：任务标题」+ ⏹ 停止按钮 | 摘要数据源 = `work.get` 任务标题 + 当前 stage；停止 = M2 agent.run 停止契约 |
| 步骤时间线（think/edit/tool 卡，可折叠，tool 聚合计数） | ✅ 既有结构化卡片流（`agent.frame`/tool/think…，`g` 跳工具卡） | 同构；聚合计数属于 adapter 归一化增量（M1.8+） |
| Changes 树：每文件 revert + 全部还原 | ✅ 改动树（M/A/D + 增减数 + 逐文件 revert + 全部还原） | 还原 = **意图**；落盘 = 人改 worktree / git 操作，与 Agent 编辑同权（ADR-002），非 Cursor 式自治 undo |
| diff 内 hunk 级接受/拒绝 | ✅ diff 审阅 hunk 分组 + y/n（j/k 选） | 本地预览标记；真正落盘 = 直接改 worktree（同权原则不变） |
| Checkpoint / Restore | ✅ 改动前快照 `ck-*` + Restore（演示） | 真实实现 = 本地 git（base_commit 对比、stash/commit 点），属 M1 已具备的 git 能力面 |
| 工具调用审批（允许一次/始终允许） | ✅ 工具卡「需要权限」+ `1`/`2` 授权 | 授权记录进会话事件流；M2 approval 契约 |
| 后台 agent + 可继续编辑 | ⏳ 分镜互斥下 Agent 在跑 = 你在 IDE 镜头干活 | 两镜头互斥天然支持"agent 后台 + 人继续 IDE"（ADR-002 不变量） |
| 行内 gutter 改动标记 / Tab 预览 | ❌ 暂不采纳 | 真实 Diff 编辑器能力归 VSCode 二开原生（IDE 镜头编辑器），非 Human Console 自有 |

**边界**：Cursor 式自治（agent 直接写盘 + 自动接受）与本设计「人手动改 worktree ≡ Agent 编辑」原则冲突，UI 只表达意图、落盘永远走 worktree/git 是同权的前提——这条不改。

## 5. 镜头切换与上下文切换（核心差异化，全部键盘一级操作）

| 键 | 动作 | 语义 |
|---|---|---|
| `⌃1` / `⌃2`（`F4` 循环） | 切镜头 | **只换这一份工作的正面/背面**：上下文不变，两镜头各自布局/焦点原样保留（有界 LRU，默认各上下文缓存最近布局） |
| `⌃K` | 开**上下文切换器** | 两个镜头里都能开；选中一项 = **换根**：目录树、编辑 tabs、Git 上下文、改动清单、转录、闸全部一起切到新任务 |
| `⌃S` | 开**会话导航器** | 列出**全部上下文的所有 Agent 会话**（`session.query`/`agent.run.query`）；Enter = 一步定位：切到该上下文 + Agent 镜头 + 选中该会话。直接回答"我有多个会话，怎么切换/怎么管理" |
| `⌃W` | 开**工作空间导航器** | 列出全部 worktree（`workspace.get`：路径/分支/dirty/归属任务）；Enter = 跳到其所属上下文。allocate/release 属 M1 契约，管理操作后续接入 |
| `⌃K`/`⌃S`/`⌃W` 输入 | 过滤 | 按 id / 标题 / 分支 / 会话 id / 路径模糊过滤（fzf 型语义，`↑↓`/`j/k` 移动，`Enter` 执行，`Esc` 关闭） |
| `Esc` | 关切换器/导航器 | 回到原镜头原焦点，零副作用 |

**上下文切换器信息行**（每行，目标是"扫一眼就认得那份工作"）：

```text
T17 · PaymentService 溢出修复        T17-impl ●dirty   IN_REVIEW   codex · 2m · 3 会话
T23 · TradeTransfer 批量核对         feat/t23         REVIEW      claude · 34m · 2 会话
T31 · JPA N+1 诊断报告                main             PLANNED     — · 昨天 · 0 会话
```

**会话导航器行**：`run-7 · codex · ACTIVE    IN_REVIEW · 2m    T17 · PaymentService 溢出修复`（跨上下文扁平清单，fzf 过滤会话 id/provider/状态/任务标题）。
**工作空间导航器行**：`/work/t17/payment-service    ●T17-impl    IN_REVIEW · 归属 T17`。

- **数据源诚实标注**：M1 无 `work.query@1`（非目标，M1-DESIGN §11）→ 首版切换器 = 用户显式打开过的上下文**本地索引**（+ 每行的分支/状态经 `workspace.get`+`agent.run.query` 刷新，N 小可接受）；**M2 读模型插件上线后**切换器改为全量活动上下文（ADR-002 架构含义表第 4/5 行）。
- **"换根"必须彻底**：切换后 IDE 镜头若还要手动导航才能回现场，即脊柱失效 → 触发证伪（§12）。

## 6. 状态与色彩语义（全局一致，两镜头共用一套）

| 对象 | 状态 | 呈现 |
|---|---|---|
| Task/WorkContext | PLANNED / IN_PROGRESS / IN_REVIEW / DONE / FAILED / ARCHIVED | stage 条：灰→青→琥珀→绿/红/灰 |
| Runtime/AgentRun | ACTIVE / DETACHED / HIBERNATED / TERMINATED / FAILED | 脊柱右侧小徽标：绿/青/蓝/灰/红 |
| Review | PENDING → APPROVED / CHANGES_REQUESTED | 琥珀 / 绿 / 红；WAIVED 显示授权者 |
| Evidence | PASS / FAIL / INVALIDATED | 绿 / 红 / 灰删除线（invalidated_at 置位后旧证据整体变灰，触发"必须重跑"视觉） |
| Action Required | 任一镜头存在待办（待审、待确认、失败） | 脊柱右端 `!n` 计数（恒常可见，不依赖轮询 UI 猜测） |

## 7. 键盘总表

**全局**：`⌃1` IDE 镜头 · `⌃2` Agent 镜头 · `F4` 镜头循环 · `⌃K` 上下文切换器 · `⌃S` 会话导航器 · `⌃W` 工作空间导航器 · `⌃\` 命令/符号浮动搜索。
**IDE 镜头**：`j/k` 树导航 · `Enter` 打开/折叠 · `h/←` 折叠或上跳父目录 · `l/→` 展开 · `Tab` 标签循环 · `[` `]` 工具窗页签 · `⌃\` 命令/符号浮动搜索 · 其余沿用 VSCode 原生键位；为从 Vim 迁移用户提供 Neovim 键位兼容层（可选映射）。
**Agent 镜头**：左侧导航树点击会话（工作区→会话两级）· `[`/`]` 循环当前工作区会话 · `g` 下一个 tool 卡片 · 点击卡片折叠/展开 · `a` 批准 review · `r` 打回 · 末行 `>` 输入 = 追问（行首标签=当前会话）· `Enter`(改动 chips) → 全宽 diff 审阅。
**切换器/导航器（⌃K/⌃S/⌃W）**：`j/k` 移动 · `Enter` 执行 · `Esc` 关闭。

（全部映射可重绑；GUI 移植时同一套语义映射为同键位，保证 muscle-memory 迁移。）

## 8. 数据映射（UI 元素 ← 数据源；UI 不拥有状态）

| UI 元素 | 数据源 | 备注 |
|---|---|---|
| 目录树 / 编辑器 | `workspace.get.path` + **本地文件系统/git**（层级目录树，折叠状态随上下文保留） | 不经 kernel 契约 |
| 编辑器内 agent 改动高亮 | `Artifact{kind=diff}.summary.files[]` + worktree 当前内容 vs `base_commit` diff | 人手动编辑后下次 collect_diff 产出新 artifact → 旧证据失效（M1-DESIGN §6） |
| Git 工具窗 | 本地 git（`base_commit`/`head_commit`/`branch`/`dirty` 亦可来自 RecoveryCheckpoint） | 提交历史可视化是 UI 阶段 |
| 转录流 | `agent.run.query` + `raw_session_ref`(blob.get) + 事件投影 | 结构化随 adapter 归一化渐次增强 |
| 改动清单 | `artifact.query`（diff 的 `summary.files[]`） | 与 IDE 高亮同源 |
| Tools/Test 结果 | `tool.run.query` + stdout/stderr blob URI | PASS/FAIL 驱动闸 |
| 证据链 | `work.get`（`evidence_refs[]`）+ `review.query`（evidence_snapshot/acceptance_results） | invalidated_at 置位即灰 |
| 完成闸 | `work.get.status` + EvidenceRefs outcome + `review.query`（§4.3 合取式逐项） | 只读呈现；DONE 只能由 workflow 经 delegation 触发 |
| 归档/恢复 | `session.query`（SessionRecord/RecoveryCheckpoint/archive_hash） | Archive 是默认清理动作，Delete 多步确认 |
| 会话条 / 会话导航器 | `agent.run.query` + `session.query`（每个 WorkContext 的 run-* 清单、状态、时长） | 会话=上下文下挂的 AgentRun；`⌃S` 跨上下文扁平枚举 |
| 工作空间导航器 | `workspace.get`（路径/分支/dirty/归属）；allocate/release = M1 契约 | `⌃W` 枚举全部 worktree；管理操作后续接入 |
| 切换器 | 本地索引 + `workspace.get`/`agent.run.query`（M1）；`work.query`+读模型（M2） | 见 §5 |

## 9. 符合性核对（对冻结硬性需求）

| 需求 | 本设计 |
|---|---|
| UX-SES-001 关 View 不杀 Runtime/历史 | 镜头只是布局组；Term 页签关窗 ≠ 进程终止（§3） |
| UX-SES-002 键盘一级切换，30+ 会话不用鼠标 | `⌃1/⌃2/⌃K`；切换器行即任务+分支+状态+时间（§5） |
| UX-SES-003 多维投影、一个 Session 一个 canonical identity | 切换器=M2 读模型的投影入口；脊柱只显示一份 canonical 状态 |
| UX-SES-004 Archive 默认清理、Delete 二级操作 | 归档=显式命令；Delete 需二次确认且受保护会话禁删（状态栏提示） |
| UX-SES-005 原生选择/复制/滚动 | 转录是结构化行而非 PTY scrollback；选择=编辑器原生 |
| UX-SES-006 结构化 Event Log 一等数据，可跳转 | `g` 跳 tool 卡片、失败测试直达（§4） |
| UX-SES-007 数百会话可检索 | 切换器过滤 + M2 读模型/FTS/Semantic（G9） |
| FR-IDE-001~006 编辑/导航/git/测试/调试/快捷键 | IDE 镜头树+编辑+工具窗+调试= VSCode 原生（Electron + 扩展生态，JDTLS/Spring Boot Tools 直接可用）；IDEA 风格 keymap 兼容层由键位映射实现 |
| FR-AG-002/005 多会话并行、变更可追踪 | 每上下文独立镜头组 + 改动高亮 + evidence 链（§8） |
| FR-SES-009 断网/客户端退出 ≠ Runtime 终止 | 镜头与进程生命周期全解耦（P9/P15） |
| ADR-002 证伪① 持续想同屏 → 分屏 | §12 证伪条件（c）持续监控 |

## 10. 分阶段落地（每阶段都有可验收目标）

| 阶段 | 交付 | 验收 |
|---|---|---|
| **UI-1 最小可用** | IDE 镜头（层级树+编辑+Term+Git diff）+ Agent 镜头（**工作区/会话导航树**+对话流水+上下文条+闸+review）+ `⌃1/⌃2/F4/⌃K/⌃S/⌃W` + 单上下文（LRU=1）+ 工作区内多会话切换 | **同一任务全程只用一个 VSCode 窗口实例完成**（对照 M1.9 验收链，不开第二个应用）；切镜头/切会话后状态原样 |
| **UI-2 会话级可用** | 结构化转录卡片、内联 diff 审阅、Problems/Tests 页签、上下文 LRU 布局缓存、`!n` Action Required | 30+ 会话：`⌃K` 换根 <0.5s 且回镜头不重导航；错误证据灰显 |
| **UI-3 大规模** | M2 读模型接入切换器全量枚举、FTS/语义检索入口、Archive/Hibernate 操作面 | G9/UX-SES-007 验收（数百 Session 定位 <1s 交互成本） |
| **UI-4 终端伴侣版（可选）** | 同一内核/投影契约下的 Neovim/TUI 第二壳（SSH/极轻场景），VSCode fork 降级为桌面选项 | 键盘映射逐项等价；脊柱/两镜头/切换器语义不变（仅当真实场景出现才启动） |

依赖标注：回复行真发送 → M2（agent.run 追加消息契约）；切换器全量枚举 → M2 读模型；转录结构化卡片 → adapter 归一化进度（M1.8+ 方向）。

## 11. NON-GOALS（第一版明确不做）

- 不重写编辑器/终端/git/LSP（VSCode 官方主仓库是宿主，能力一律 REUSE；fork 只改壳与镜头，不改编辑内核）。
- 不做两个镜头同屏分格的"IDE 内嵌 Agent 面板"或"tmux 四格"（ADR-002 已否决：会退化成"两个都不够用"）。
- 不做完整鼠标级 GUI、浏览器/远程视图、插件市场、主题市场。
- 不做编辑器布局/会话的持久化导出迁移（视图状态是派生数据，随上下文 LRU 保留即可）。

## 12. 证伪条件（这设计怎么算错了，持续监控）

1. 用户在真实任务中**持续**尝试把两个镜头并排同屏 → "全屏互斥 + 瞬间切换"的前提错，改为可分屏。
2. 切上下文后，人**仍需手动导航**才能回到 IDE 现场 → 脊柱/换根没生效（§5）。
3. `M1.9` Console 读投影充分性验收失败 → 两镜头拿不到数据，设计不成立。
4. 30+ 会话下 `⌃K` 从唤起到换根 >1s 或依赖记忆位置 → 切换器设计失败，先做读模型。
5. 任一用户报告"找不到那份工作/那个 Agent 在干嘛"超过一次 → 信息层级失败，回 §6 状态呈现与 §8 数据映射返工。