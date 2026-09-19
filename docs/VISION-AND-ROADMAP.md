# Agent-Native Engineering Work OS — 项目远景与路线图（VISION-AND-ROADMAP）

> 本文档是本仓库的**目标基线**：把散落在历史素材（需求基线 docx、总体设计与各详细设计、AEOS Rust 原始仓库、微内核各版本仓库）中的项目远景、中期目标与长期目标收敛为一份可审计、可引用、可执行的文件，避免方向不明。
>
> - 状态：**基线入档（2026-09-19）**，源自冻结的需求基线 v0.4 / 总体设计 v0.1 / AEOS Rust M0–M10 实现史。
> - 配套：`docs/M1-DESIGN.md` 是当前 Go 工程的**实现基线**；本文件回答"为什么做、终点在哪"，M1-DESIGN 回答"眼前这一段怎么做"。
> - 维护约定：本文档只随时间轴**增补**（新里程碑结论、新 ADR）；冻结的蓝图内容（愿景/目标/原则/里程碑表）不做静默改写，改动须另走 ADR。

---

## 1. 源材料清单（traceability）

| 来源 | 内容 | 在本文档中的角色 |
|---|---|---|
| `Agent-Native软件工程工作台-需求分析与总体方案基线-v0.1.docx`（2026-08-27） | 愿景雏形："把 IDE 从开发系统本体降级为可替换交互客户端" | 愿景演进起点 |
| `Agent-Native软件工程工作系统-需求分析与总体方案基线-v0.3.docx`（2026-08-27） | 范围边界 + 核心对象/状态机草案 | 中期目标边界参照 |
| `Agent-Native软件工程工作系统-需求分析与总体方案基线-v0.4.docx`（2026-08-27，**冻结**） | 愿景/13 目标/三层模型/15 原则/Phase 0–6/MVP/成功指标 | **远景与路线图权威来源** |
| `Agent-Native软件工程工作系统-总体设计-v0.1.docx`（2026-08-27） | 交付里程碑 M0–M7、MVP 主链、Exit Criteria | 设计层里程碑权威来源 |
| 5 份 P0 详细设计（Canonical-Event / Core-Domain+SQLite / Harness-Adapter-SDK / Runtime-Supervisor / Session-Archive+Recovery，v0.1） | 中期可实现性的落地约束 | 中期目标实现参照 |
| `agent-native-engineering-os-m10/`（Rust 单体，M0–M10 累积实现）+ Downloads 中 m1–m8 README/CHECKPOINT/API 文档 | AEOS Rust 原始实现史 | 路线图"已验证"实证 |
| `agent-native-microkernel-v0.9.2-client-auth` / `-v1.0` / `-v0.10.0-adversarial-qualified`（Go 参考实现） | 微内核链演进 | 当前工程前史 |
| 本仓库 `docs/M1-DESIGN.md`、`kernel/docs/13-…` 等 | 当前 Go 工程实现现状 | 当前状态锚点 |

---

## 2. 项目远景（Vision）

### 2.1 一句话愿景（v0.4 §1.1）

> **先用大型软件工程工作流证明 Core Kernel**（Work Context、Agent Runtime、Workflow、Scheduler/Event、Notification、Memory/Knowledge、Search/Retrieval、Connector）。工程域稳定后，再以插件化方式承载学习计划、知识复习、主题沙龙、跨学科连接、运动与饮食等个人工作流，**而不让这些扩展反向污染工程核心**。

### 2.2 最终形态（v0.4 §18）

这不是 Vim 配置，不是"自己写的 Cursor/IDEA"，也不是把生活场景塞进同一 UI 的超级 App。它是一套 **Agent-Native Engineering Work OS**：

- **平台持有工作状态与历史资产**，Runtime 按需存在，View 随时可替换；
- **Agent 负责执行与推理，确定性工具提供 Truth，Workflow 提供治理**；
- **Raw History 复利为长期知识**（Source → Derived → Curated 三层资产模型）；
- 软件工程先做到**可替代并超越重型 IDE**，再以 Extension Pack 验证更广泛的 Agentic Work Runtime；
- 成功标准不是"比 IDEA 省内存"，而是让软件工程从 IDE-centric 演进为 **Agent-native**：工作可编排、Session 可安全恢复、Agent 可替换协作、工具提供可信事实、交付流程可验证、每次工作历史沉淀为未来 Agent 可检索可追溯的工程知识资本。

### 2.3 三层模型（v0.4 §3.3，防发散的范围护栏）

| 层级 | 内容 | 定位 |
|---|---|---|
| **Core Kernel** | Work/Context、Agent Runtime、Workflow、Scheduler/Event、Notification、Memory/Knowledge、Search/Retrieval、Connector、Artifact/Storage、权限 | 平台通用内核；**必须稳定、领域无关** |
| **Engineering Core Domain** | Editor/Terminal Views、Git/Worktree、Java/Spring/JPA 语义工具、Build/Test、Review、PR/CI/Release、工程 Evidence | **第一核心域，当前 MVP 主战场** |
| **Personal Workflow Extensions** | 学习计划/间隔复习/主题沙龙、时间管理、运动/食谱等 | **后续 Extension Pack；非当前 MVP，不得拖慢工程主线**（FR-EXT-EVAL-001：新领域必须是 80% 能力可复用 Core Kernel 才允许引入） |

### 2.4 设计原则（v0.4 §4，P1–P15 摘要）

IDE-independent（P1）· LLM Reasoning, Tools Truth（P2）· Workflow First（P3）· Agent as First-Class Worker（P4）· Reproducible Engineering（P5）· Evidence over Guess（P6）· Progressive Replacement（P7）· Open & Replaceable（P8）· **Persistent Work Context over Persistent Process**（P9）· Session Safety by Default（P10）· History Compounds into Knowledge（P11）· Harness-Neutral Memory（P12）· **Human operates Work; Agents execute Work; Tools establish Truth; UI provides Views**（P13）· Core Kernel, Domain Packs（P14）· State Lives in the Platform, not in Views（P15）。

### 2.5 产品目标（v0.4 §3.1，G1–G13）

| 目标 | 内容 |
|---|---|
| G1 | 提供日常 Coding/阅读/导航/搜索/Diff/Git/终端/调试，满足长期主力使用 |
| G2 | 完整接入 Codex/Claude/Gemini/OpenCode/内部 Agent，Agent 可随时替换 |
| G3 | 开发任务分组、Agent 角色分工、多会话、多 Worktree、上下文绑定、状态追踪与恢复 |
| G4 | 确定性工具补齐 IDEA 高级语义：Spring Bean/AutoConfig/SpEL/Event、JPA/Hibernate/Spring Data、跨模块影响分析、结构化重构 |
| G5 | "计划→实现→编译→测试→静态/运行时验证→Review→Merge Gate"可编排工程工作流 |
| G6 | 同一语义能力同时供 Human Console、Agent、CLI、CI 使用，能力不锁死在编辑器 UI |
| G7 | 特定领域超过 IDEA：可解释运行时事实、业务语义图谱、多 Agent 并行、可重放重构、自动证据链、项目知识沉淀 |
| G8 | Work Context 一等公民；Agent 进程/PTY/UI View 可随时结束，任务上下文/Session 历史/Workspace 状态/Artifacts/Evidence 可恢复 |
| G9 | 大规模 Session Knowledge Navigator：按项目/目录/Repo/Task/Branch/Worktree/Topic/Agent/Team/日期/状态/MRU 多维组织与切换 |
| G10 | Engineering Memory & Knowledge Asset Plane：Raw Session 长期保存、Derived 可重建、验证过的知识带 provenance 持续积累 |
| G11 | 覆盖完整工作流：采集/工单同步 → Triage → 需求分析 → 设计 → 实现 → 测试 → Review → Commit/PR/CI → Release/验证 → 知识沉淀 |
| G12 | Scheduler、Event Bus、Notification Center、Automation Rules 为平台原生能力 |
| G13 | 内核领域无关，Personal Workflow 以 Extension Pack 接入；**不属当前核心 MVP** |

---

## 3. 路线图：三套"里程碑编号"澄清（重要）

历史素材里存在**三套不同的里程碑编号**，此前是方向不明的首要来源。本文档予以正式对齐：

| 编号体系 | 出处 | 语义 | 现状 |
|---|---|---|---|
| **设计层 M0–M7** | 总体设计 v0.1 §18.2（Contract Skeleton → Work+Session Core → Runtime+Harness A → Harness B+Navigator → Workflow+Git → Neovim Client → Spring Evidence Slice → Hardening） | MVP 交付顺序的蓝图 | 已被 AEOS Rust 实现史吸收、扩展 |
| **AEOS Rust M0–M10** | `agent-native-engineering-os-m10/` 累积实现 + m1–m8 文档 | 原版 Rust 单体的实际实现轨迹 | **已实现**（M0–M10），是本工程的"前身实证" |
| **当前 Go 工程 M0.5 / M1** | 本仓库 `kernel/`（M0.5 微内核对抗认证）+ `docs/M1-DESIGN.md`（M1.0–M1.9 竖切） | 微内核化重写的近期实现 | M0.5 认证通过；M1.0–M1.8.5 done；**M1.9 在途** |

> **结论**：当前仓库的 "M1"（工程竖切）**不是** AEOS Rust 的 "M1"；本工程 M1 之上仍在推进的完整领域模型迁移即对应 AEOS 蓝图中的 M2+（见 §5）。引用历史里程碑时必须带前缀（"AEOS M9" 或 "设计层 M3"），避免与当前 M1.x 混淆。

### 3.1 AEOS Rust 实现史（路线图"已验证"的实证，M0–M10）

| 里程碑 | 一句话内容 |
|---|---|
| AEOS M0–M1 | durable continuation slice：WorkContext/SessionRecord 长期身份、raw evidence、archive generations、continuation identity；Runtime 进程可销毁 |
| AEOS M2 | provider binding：首个 Harness adapter 路径 + 更强持久化规则 |
| AEOS M3 | session space + action inbox + canonical projections：会话只读读模型（哪些需要 attention、属于哪个 repo/branch） |
| AEOS M4 | live session workspace：TUI/terminal 只是可 attach 的 View，Runtime-backed AgentRun |
| AEOS M5 | persistent Session Workspace：WebSocket 流式输出、InputLease（多观众一写者） |
| AEOS M6 | Session Tree、Presence、Structured Approval、Neovim 客户端 |
| AEOS M7 | durable engineering workflow control plane（任务交付状态机） |
| AEOS M8 | deterministic engineering tool service：Build/Test/Verification 由 ToolRun 执行并生成 evidence，Gate 消费工具证据 |
| AEOS M9 | durable ToolRunner shim：Core/UI 消失 Build/Test 继续；capture-before-exec、seal-manifest |
| AEOS M10 | structured test（JUnit/Surefire 解析）、Git push、GitHub PR SCM connector、CI 事实接入（Schema v13） |

（AEOS Rust 的 M11 蓝图仅以"10 条 gate 谓词"等提法见于 ADR-001 的参照描述，未形成独立实现仓库。）

### 3.2 设计层交付里程碑（总体设计 v0.1 §18.2，M0–M7）

Contract Skeleton → Work+Session Core（Asset Store/FTS/backup）→ Runtime+Harness A（PTY supervisor/attach/archive/resume）→ Harness B+Navigator（capability matrix/MRU/projection/Action Required）→ Workflow+Git（Worktree、Analyze→Implement→Test→Review、Gate、scheduler）→ Neovim Client（**改向：VSCode 官方主仓库二开，见 ADR-004**）→ Spring Evidence Slice（`spring.why_bean` static+runtime）→ Hardening（crash recovery/backup restore/30+ sessions UX）。

**MVP Exit Criteria（§18.3）**：两个差异明显的 Harness 过同一 Adapter Contract；30+ Session 键盘级 MRU/投影/搜索且关 View 不丢 Runtime/History；Runtime 终止后可 Semantic Resume 并恢复 Task/Git/Artifact 上下文；真实需求 Analyze→Implement→Test→Review→Finalize 全过 Review Gate；Raw/Canonical 可备份校验、Derived 可重建；`spring.why_bean` 区分静态推断与运行时证据并被 Agent Skill 调用；全程不依赖 IDEA。

### 3.3 当前 Go 工程状态（锚点）

- 微内核链：v0.9.2（client auth）→ v1.0（可执行参考）→ **v0.10.0 M0.5 对抗资格认证 PASSED**（`kernel/docs/13-…`；非生产安全认证）。
- M1 竖切：M1.0–M1.8.5 全部 `— done`（tags 至 `m1.8.5-workspace-by-context-recovery`）；**M1.9（真实 codex + Maven 端到端 + G1–G6）在途**，`scripts/qualify-m1.sh` 尚未实现。
- 近期成败只有一件事：**M1.9 qualification 通过，输出 `M1 ENGINEERING VERTICAL SLICE: PASSED`**（判据见 `docs/M1-DESIGN.md` §2/§10/§13）。

---

## 4. 实施路线（v0.4 §14，Phase 0–6）

| 阶段 | 内容 | 与本文档目标的关系 |
|---|---|---|
| Phase 0 体验验证 | **VSCode 官方主仓库二开工作台**（改壳：脊柱+两镜头，规格见 `prototypes/human-console.html`）+ Core CLI/Daemon 原型；接 2 个 Agent CLI；验证"日常不开 IDEA" | 体验壳，禁止把状态写进 dotfiles/tmux/前端内存（[ADR-004](./ADR-004-vscode-fork-ui-carrier.md)） |
| Phase 1 Agent Control Plane MVP | Task/Session/Role/Worktree/状态；Analyze→Implement→Verify→Review；最小 Evidence；Work Context 与 Runtime 分离；Navigator MVP；Archive 优先/Delete 多步确认 | ≈ 设计层 M1–M4，≈ AEOS M1–M7 |
| Phase 2 Java/Spring 语义纵向切片 | Bean Why、Event Graph、Impact Analysis 三类高价值查询 + runtime evidence | G4/G7（中期主线） |
| Phase 3 语义重构与 JPA | OpenRewrite/AST/LST 重构引擎；Entity/Repository/Query/DB schema 模型；重构纳入 compile/test/review gate | G4/G5/G7（中期） |
| Phase 4 统一 Semantic Graph 与项目知识 | Java/Spring/JPA/DB/Test/Task/Requirement 统一图谱 + 项目特有业务语义 + 历史设计知识 | G7/G10（中长期） |
| Phase 5 Engineering Memory & Continual Learning Readiness | Raw Archive + Canonical Event（≥2 种 Harness 格式）；冷热分层；Runtime/Semantic Resume；跨 Agent Continue；Knowledge Promotion/provenance/supersedes | G10/G11/G12（长期地基） |
| Phase 6 Personal Workflow Extension Validation（可选） | 学习+间隔复习+主题沙龙 → 跑步+天气 → 饮食；**不作为 v1.0 前置** | 长期 Extension Pack 验证 |

---

## 5. 中期目标（M2 及以后 / Phase 2–5）

> 中期 = 在 M1 竖切证明平台骨架后，把 Engineering Core Domain 做深做透，直到"可替代并超越重型 IDE"在该领域成立。当前 Go 工程尚无独立的 M2 实现文档；本节是目标定义，M2 立项时应依 `docs/M1-DESIGN.md` 的范式另行建立**可证伪命题 + Gate 判据 + 里程碑拆分**。

1. **完整领域模型迁移（AEOS 蓝图 M2+）**：把 AEOS Rust 已验证的完整领域模型（WorkItem/Task/WorkContext/SessionRecord/AgentRun/Evidence/Review Gate/Knowledge Asset/事件流/Archive-Recovery）迁移到微内核插件边界之上，每块都遵循"原子插件 + 各自契约 + 各自 authority"（C09/C32），不回流 kernel。
2. **Java/Spring/JPA 确定性语义工具（G4）**：静态模型 + Runtime ApplicationContext/ConditionEvaluation + DB schema 三方交叉验证；`why-bean`/Event Graph/Impact Analysis 先行；结论带证据级别 PROVEN/INFERRED/POSSIBLE/UNKNOWN。
3. **可重放、可验证的跨模块语义重构（G5/G7，Phase 3）**：Intent → Impact Plan → Semantic Transform（OpenRewrite/AST/LST）→ Compile → Test → Residual Search → Review+Human Gate；绝不做直接大范围 search/replace。
4. **工程工作流与外部交付链打通（G11，AEOS M10 方向）**：Commit/PR/CI/Release/验证与远程事实（JUnit、Git push、PR、CI）进入同一 Evidence/Gate 体系。
5. **Session Navigator 与大规模导航（G9/UX-SES 系列）**：多维投影（Project/Repo/Task/Branch/Worktree/Topic/Agent/Team/Date/Status/MRU）、MRU 一级键切换、FTS 先行 + 语义检索后置；30+/100+/500+ Session 可用性验收。
6. **Scheduler / Event Bus / Notification / Automation（G12）**：统一事件（SESSION_COMPLETED/APPROVAL_REQUIRED/TEST_FAILED/REVIEW_REQUIRED…）+ 通知中心 + 自动化规则；所有自动化产物进入同一 Work Context。
7. **多 Harness + Agent Team（G2/G3，FR-HAR 系列）**：第二 Harness 验证 Adapter 抽象不是"只适配 Codex"；implement/review/research/debug/test/integrator 角色与父子 Agent；写 Agent 默认独立 Worktree。
8. **Memory/Knowledge 资产化（G10，Phase 5）**：Raw Session 永不因省资源而删；Canonical Event 版本化；Source/Derived/Curated 三层；Knowledge Promotion 与 supersede/stale 治理；Semantic Resume 跨 Harness Continue（FR-HAR-005）。

---

## 6. 长期目标（Phase 4–6 及其后）

1. **Engineering Work OS 成立（"可替代并超越重型 IDE"）**：以 §16 成功指标为准绳——IDEA 启动频率趋近 0、Agent 承担任务比例持续提升、人类转向需求/架构/Review/风险、结论可解释、变更可追溯、平台可替换、历史可找回、知识可复用、UI 故障不丢资产。
2. **统一 Semantic Graph 与工程知识系统（G7/G10，Phase 4）**：代码 + Spring + JPA + DB + 测试 + 业务概念 + 任务/需求统一图谱；Agent 从"临时搜索项目"升级为"查询持续演进的工程知识系统"；失败过程、人类纠正、被否决方案被长期保留（"为什么不能这么做"常比结论更有价值）。
3. **Continual Learning Readiness（Phase 5 末态）**：原始 Session/Canonical Events 作为模型评估、持续学习与知识治理的原始资产底座；更换摘要/Embedding/抽取器后可基于 Raw Source 重建 Derived/Curated。
4. **通用 Agentic Work Runtime（v0.4 §18 更长期）**：同一 Core Kernel 在不破坏工程主线的前提下自然承载学习、时间管理、运动、饮食等 Personal Workflow Extension Pack（学习：间隔复习/主题沙龙/跨学科连接；运动：计划+天气 Connector 动态调整；饮食：记录与回顾，非医疗诊断）。**这是平台成熟后的扩展价值，不是当前阶段的范围负担。**

---

## 7. 未决决策（v0.4 §17，方向定型前的关键问号）

- Core 部署形态：单机 daemon + SQLite 起步 vs 直接 client/server（建议先单机，但 API/identity 不绑定单进程）。
- 平台核心 CLI/项目命名与仓库边界：单仓 vs core+adapters+skills 多仓。
- Control Plane 状态存储：本地文件/SQLite/嵌入式 DB，及与 Git 的关系。
- Canonical Event Model 最小 schema 与版本升级策略（哪些原始 payload 永不丢弃）。
- Work Context 与 Task 边界：一个长期 Context 是否含多个 Task/Epic，何时 seal/archive。
- Harness Adapter 第二批目标；Session Runtime 底座（PTY supervisor / Zellij / WezTerm / tmux / 自研 daemon）。
- Semantic Graph 首期是否落库；Spring Runtime Evidence 采集方式（Actuator/测试启动器/Java Agent/专用探针）。
- Raw Archive 默认存储（本地 SSD + NAS/WebDAV/S3）、加密/保留/隐私删除策略。
- Knowledge Promotion 哪些步骤自动化、哪些必须 Human Gate。
- 长期产品边界：工程稳定后是否将通用 Core Kernel 独立命名/拆仓，让 Learning/Personal Packs 与 Engineering Pack 平级演进。

---

## 8. 一张图：从远景到当前（导航）

```text
最终愿景：Agent-Native Engineering Work OS（G1–G13，P1–P15）
   │
   ├─ 中长期：统一 Semantic Graph + Engineering Memory/Knowledge + Continual Learning
   │          └─ Phase 2–5（语义工具 → 语义重构/JPA → 图谱 → Memory & Knowledge）
   ├─ 中期：Engineering Core Domain 做深（G4/G5/G9/G10/G11/G12）+ 多 Harness/Agent Team（G2/G3）
   ├─ 近期：M1 竖切（G8/G6/G5 骨架）—— 当前 Go 工程（M1.0–M1.8.5 done，M1.9 在途）
   └─ 地基：微内核 M0.5 PASSED（领域无关 Kernel，词汇护栏机器强制）
           └─ 参考实证：AEOS Rust M0–M10（同一蓝图的原版实现史）
```