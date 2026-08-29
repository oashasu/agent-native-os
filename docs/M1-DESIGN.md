# M1 — Engineering Vertical Slice 设计

日期：2026-08-29
状态：**草案，待评审**
前置：`REVIEW-microkernel-v0.10.0.md`（A2/A3）、`FIX-PLAN`（P0 已完成）、`ADR-001`（Go）
方向输入：项目所有者 + Codex（Q1=A / Q2=Real Harness + mock 双轨 / M1 Architecture Gate）

---

## 1. M1 要证明的唯一命题

> **一条真实工程工作流可以完全建立在 kernel 的 capability / plugin contract 之上，
> 不需要任何 engineering-specific 语义回流 Microkernel。**

不证明"AEOS 完整领域模型已迁移正确"（那是 M2+）。不证明"GitHub/CI/Release 打通"（M3）。
只证明骨架成立：真实 Agent 通过外部能力链完成一次真实工程变更，且 Runtime 消失后状态不丢。

## 2. M1 Architecture Gate（最高优先级判据）

> **如果为了跑通这条工作流，需要往 Microkernel 增加 `Task / WorkContext / Git / Worktree /
> Agent / AgentRun / Build / Test / Review / Workflow` 等工程领域语义，则 M1 失败。**
> 这些能力必须通过既有通用 contract / capability / event / stream / delegation 机制实现。

**执行方式：**
- `architecture-tests/check_boundaries.py` 的 kernel 词汇黑名单已禁 `Task/Session/Agent/Workflow/Git/...`；M1 期间不放宽。
- FIX-4 composition fitness function 随 `engineering-workflow` 插件落地（见 §7、FIX-PLAN）。
- 每个 M1 里程碑结束做一次人工 gate review：kernel `git diff` 是否只动了 mechanism。
- kernel 代码在 M1 期间**只接受 FIX-PLAN 里的 P1/P2 硬化项**，不接受任何新的领域概念。

## 3. 锁定的工作流链（线性，无 stage machine）

```text
Task 规格（goal + scope + acceptance_criteria）
        │
        ▼
work.create ──────────────► Task + WorkContext（status=PLANNED）
        │
        ▼
workspace.allocate ───────► git worktree + branch（base_commit 记录）
        │
        ▼
work.transition(IN_PROGRESS)
        │
        ▼
agent.run（真实 Harness，streaming）
        │   ├── source changes（写进 worktree）
        │   ├── agent.frame stream（stdout/stderr/status）
        │   └── raw_session_ref（原始转录）
        ▼
artifact.collect_diff ────► Artifact{kind=diff, sha256, summary}
        │
        ▼
tool.run(label=build) ────► ToolRun{exit_code, stdout_ref, ...} → outcome PASS/FAIL
        │
        ▼
tool.run(label=test) ─────► ToolRun → outcome PASS/FAIL
        │
        ▼
work.attach_evidence ×N ──► WorkContext.evidence[] += {kind, source_id, outcome}
        │
        ▼
work.transition(IN_REVIEW)
        │
        ▼
review.submit ───────────► Review{decision, diff_artifact_id, evidence_snapshot}
        │
        ▼
[DONE gate 求值：见 §4]
        │
        ▼
work.transition(DONE)
        │
        ▼
session.seal ────────────► SessionRecord + 不可变 archive（raw + canonical events + recovery meta）
        │
        ▼
workspace.release（策略：失败时保留）
        │
        ▼
agent.run 的 Runtime 终止
```

每一步之间由 `engineering-workflow` composition 插件驱动，并向 `event.journal.append` 写一条 canonical 事件（§9）。

## 4. DONE 不变量

```text
DONE :=
      acceptance_criteria 已满足（人工/review 判定）
  AND build evidence.outcome == PASS
  AND test  evidence.outcome == PASS
  AND review.decision == APPROVED
  AND review.diff_artifact_id == 当前 agent.run 产出的 diff artifact
```

**强制点分层：**
- **状态机**（PLANNED→IN_PROGRESS→IN_REVIEW→DONE，禁跳步 + 乐观版本）：`work-registry` 的 `work.transition@1` 强制。
- **证据 gate**（上面的合取式）：`engineering-workflow` 插件作为**读模型**求值 —— 查 build/test ToolRun outcome + review decision + diff 绑定，全 PASS 才调 `work.transition(DONE)`。
- **review 与 diff 绑定**（AEOS M11 的"当前 attempt gate"教训，廉价引入）：`review.submit` 必须携带 `diff_artifact_id`；gate 校验它等于当前 agent.run 的 diff。diff 变了，旧 approval 失效。
- **硬化（非 M1）**：`work.transition(DONE)` 要求携带 review-gate 签发的授权 token，kernel 无关、纯插件间。列入 M2。

M1 Architecture Gate 保证这条逻辑**只活在 `engineering-workflow` 一个插件里**，独立可测，不渗进 kernel 也不摊进原子插件。

## 5. 插件 & 契约地图

契约放 **仓根 `contracts/`**（不是 `kernel/contracts/`，见 FIX-5）。命名沿用 `org.vibe.*` / `capability@major`。

### 5.1 原子能力插件（持有 canonical state）

| 插件 | authority | 契约（kind） | 拥有 |
|---|---|---|---|
| `org.vibe.work.registry` | `work-main` | `work.create@1`(cmd) `work.get@1`(query) `work.transition@1`(cmd) `work.attach_evidence@1`(cmd) | Task、WorkContext、生命周期状态机、证据 provenance |
| `org.vibe.workspace` | `workspace-main` | `workspace.allocate@1`(cmd) `workspace.release@1`(cmd) `workspace.get@1`(query) | Workspace、git worktree、branch |
| `org.vibe.agent.harness` | `agent-runs-main` | `agent.run@1`(cmd, streaming) `agent.run.get@1`(query) `agent.run.cancel@1`(cmd) | AgentRun、harness 生命周期归一化、raw session 引用 |
| `org.vibe.artifact` | `artifact-main` | `artifact.collect_diff@1`(cmd) `artifact.get@1`(query) | Artifact（diff / 命令输出），内容寻址 |
| `org.vibe.tool.runner` | `toolruns-main` | `tool.run@1`(cmd) `tool.run.get@1`(query) | ToolRun、确定性命令执行、build/test 结果 + 指纹 |
| `org.vibe.review` | `reviews-main` | `review.submit@1`(cmd) `review.get@1`(query) | Review 决定、gate 状态、evidence 快照 |
| `org.vibe.session` | `sessions-main` | `session.seal@1`(cmd) `session.get@1`(query) | SessionRecord、raw 归档、per-session canonical 事件切片 |

### 5.2 Foundation（复用 kernel 现有）

| 契约 | 用途 |
|---|---|
| `event.journal.append@1` / `event.journal.replay@1` | 工作流里程碑 canonical 事件（§9）；`workflow.get` 的投影源；`session.seal` 的事件切片源 |

### 5.3 Composition 插件（只做编排）

| 插件 | 契约 | 特性 |
|---|---|---|
| `org.vibe.workflow.engineering` | `workflow.engineering.run@1`(cmd, 长 deadline) `workflow.engineering.get@1`(query) | **无私有 canonical state**（见 §7）；`consumes` 全部上面的能力 + `event.journal.*`；`manifest.composition = true` |

## 6. 数据模型

比 v0.10 的 `Work{ID,Title,Status,Version}` 富，一次到位以便自然扩成 AEOS。

```text
Task
├── id
├── title
├── goal                     # 自然语言目标
├── scope                    # 允许改动范围描述
├── acceptance_criteria[]    # 结构化验收项 {id, text, satisfied:bool}
├── status                   # PLANNED|IN_PROGRESS|IN_REVIEW|DONE|FAILED
├── version                  # 乐观并发
└── work_context_id

WorkContext
├── id
├── task_id
├── repo                     # 源仓库路径（M1：本地路径）
├── workspace_ref            # {id, path, branch, base_commit} | null
├── agent_run_ids[]
├── artifact_ids[]
├── evidence[]               # {kind: build|test|review, source_capability, source_id, outcome: PASS|FAIL}
├── review_ids[]
├── session_ids[]
└── version

AgentRun
├── id
├── work_context_id
├── workspace_ref
├── prompt
├── provider                 # "mock" | "<discovered real provider>"
├── harness_native_id        # provider 私有会话标识（external metadata，C32）
├── status                   # RUNNING|COMPLETED|FAILED|CANCELLED|TIMEOUT
├── raw_session_ref          # 指向 session 插件归档前的热态转录
├── provider_metadata        # provider 私有字段 blob
├── started_at / ended_at

Artifact
├── id
├── work_context_id
├── kind                     # "diff" | "command_output"
├── sha256                   # 内容寻址
├── summary                  # diff: {files_changed, insertions, deletions, files[]}
└── created_at

ToolRun
├── id
├── work_context_id
├── workspace_ref
├── label                    # "build" | "test"（调用方标注用途）
├── command[]
├── exit_code
├── outcome                  # PASS(exit 0) | FAIL
├── stdout_ref / stderr_ref  # 内容寻址
├── fingerprint              # command + env + workspace base_commit 的哈希
├── started_at / ended_at

Review
├── id
├── work_context_id
├── agent_run_id
├── diff_artifact_id         # 绑定：approval 不可跨 diff 复用
├── decision                 # APPROVED | CHANGES_REQUESTED
├── reviewer                 # 身份（M1：CLI 调用者 / 显式传入）
├── notes
├── evidence_snapshot[]      # 提交 review 时看到的 {kind, outcome}
└── created_at

SessionRecord
├── id
├── work_context_id
├── agent_run_id
├── archive_ref              # 不可变归档目录/对象（raw 转录 + canonical 事件切片 + recovery.json）
├── event_range              # [from_seq, to_seq] in event journal
├── sealed_at
```

各插件私有 schema（C09）。M1 存储实现：SQLite/WAL + 增量写（**不**抄 v0.10 work-registry 的全量 JSON blob，见 FIX-15）；内容寻址对象存 `assets/objects/sha256/...`（沿用 AEOS M1 布局）。

## 7. 编排模型 —— 解决 A2

**决定：`engineering-workflow` 是无状态同步编排器（A2 的方案 a，加强版）。**

- `workflow.engineering.run@1` 是一次**同步长 deadline** 命令（默认 30min，可配）。整条链在这一次
  调用内跑完 —— 因此 kernel 的请求作用域 delegation 全程有效，不存在"返回后异步丢授权"问题。
- 工作流**不持有任何私有 canonical state**。每步向 `event.journal.append` 写 canonical 事件。
- `workflow.engineering.get@1` = 对 `event.journal.replay@1` 按 `work_context_id` 过滤 + 投影出
  管线进度。进度是**可重建的派生数据**（C19），不是 workflow 私有状态。
- 崩溃语义：run 中途挂掉 → 各原子插件的 state 已持久（work/workspace/agent_run/artifact/toolrun
  按已完成的步数落库），journal 显示走到哪一步。**重跑是 M1 的人工决定**，无自动 reconciler。
- **不需要 `service_authority`**：`workflow.engineering.run` 由 CLI 身份发起，携带 delegation；
  所有子调用（work.* / workspace.* / agent.run / tool.run / artifact.* / review.* / session.seal /
  event.journal.append）走 delegation scope。CLI 身份的 policy grant 里显式列出这批 child 能力。

**M2 硬化（非 M1）**：改为 `service_authority` + 持久编排状态机 + 后台 reconciler，支持自动续跑 / rework 回环。

**为什么这样对**：这是"编排归 composition、composition 不持有 canonical state"最干净的表达 ——
workflow 插件删掉后，重建它不丢任何业务数据；它就是 journal 上的一个投影 + 一串 command。

## 8. 真实 Harness Adapter

### 8.1 Provider 中立

`agent.run@1` 契约里**不得**出现 `CodexThread / ClaudeConversation` 等私有概念。契约只认：
`prompt`、`workspace_ref`、`provider`（可选，缺省走发现）、返回 `AgentRun` + `agent.frame` stream。
Provider 私有 → `provider_metadata` / `raw_session_ref`。

### 8.2 运行时发现

`agent-adapter` 启动时探测：`codex --version` / `claude --version` / ...。可用且自动化接口最稳的
作为 M1 的 real provider #1。**M1 只接一个真实 provider**，不做 Codex/Claude/Gemini 横向铺开
（那是 adapter abstraction 的第二次验证，M2）。

这台开发机可用的 CLI 待确认（`which claude codex` / `--version`）。

### 8.3 双轨测试

```text
                  agent.run@1 契约
                        │
          ┌─────────────┴─────────────┐
     mock provider                real provider #1
     （长期保留）                  （M1 qualification）
          │                            │
   确定性，CI regression        codex / claude CLI
   + 故障注入：                  端到端真实变更
     completion / failure /
     timeout / approval-required /
     malformed event / crash /
     restart / partial output /
     cancellation
```

`mock-agent` 从 v0.10 迁移进来并**长期保留**为 fixture。M1 的 `scripts/test.sh` 用 mock 跑
regression；qualification 场景（§10）用 real provider 跑一次。

## 9. Canonical 事件清单（写入 `event.journal.append`）

每条带 `work_context_id`（在 payload），`Source` 标注产出插件，`trace_id` 贯穿整条 run。

```text
work.created
workspace.allocated
work.transitioned          {from, to}
agent.run.started          {agent_run_id, provider}
agent.run.completed        {agent_run_id, status}
diff.collected             {artifact_id, files_changed, insertions, deletions}
tool.run.completed         {tool_run_id, label, outcome, exit_code}
evidence.attached          {kind, source_id, outcome}
review.submitted           {review_id, decision}
work.done                  {work_context_id}
session.sealed             {session_id, archive_ref}
workspace.released         {workspace_id}
```

`session.seal` 用 `event.journal.replay@1` 拉取本 run 的 `[from_seq, to_seq]` 切片进归档。

## 10. Qualification 场景 —— "M1 ENGINEERING VERTICAL SLICE: PASSED" 的定义

准备一个小而真实可编译的仓库 `fixtures/sample-java-project/`（Maven，含 `Calculator.add` + 现有测试）。

Task：
```text
goal: 修改 Calculator.add，对非法输入（null / 溢出）增加指定行为，并补充测试。
scope: 只允许改 Calculator.java 与其测试文件。
acceptance:
  - mvn -q test PASS
  - 新增测试覆盖非法输入行为
  - 未改动无关文件
```

完全通过平台跑：
```text
vibe task create --goal ... --scope ... --acceptance ... --repo fixtures/sample-java-project
        ↓  (work.create → WorkContext)
vibe workflow run <task-id>
        ↓  workspace.allocate (git worktree)
        ↓  agent.run  → 真实 Codex/Claude CLI，实际改文件
        ↓  artifact.collect_diff
        ↓  tool.run "mvn -q -pl . test"  (label=build 用编译；label=test 用 test)
        ↓  work.attach_evidence ×2
        ↓  review.submit --decision APPROVED   (M1：人工/半自动)
        ↓  DONE gate 求值 → work.transition(DONE)
        ↓  session.seal
        ↓  agent runtime 终止
```

然后**关键动作**：
```text
杀掉 agent runtime + 关闭 CLI client
重启 kernel
vibe task show <task-id>
```

仍然完整可见：`goal / acceptance_criteria / agent_run / worktree / diff / build evidence /
test evidence / review / session_record / raw_history_ref`。

全部满足 → **M1 ENGINEERING VERTICAL SLICE: PASSED**。

## 11. NON-GOALS（明确排除，防止 A 膨胀成 B）

```
完整 ANALYSIS/DESIGN/PLAN stage machine 与 rework 回环
Semantic Graph / Spring why-bean / JPA 语义模型
GitHub PR / webhook / CI connector / Release 验证
Knowledge Promotion / Vector Search
Desktop UI / 完整 Neovim UI
多 Agent Team / 多 Provider 生产级支持
自动 workflow reconciler / 自动续跑
分布式 / 多 host authority
```

## 12. 需在评审确认的设计判断

| # | 判断 | 备选 |
|---|---|---|
| D1 | `engineering-workflow` 无状态、同步长 deadline、进度=journal 投影（§7） | service_authority + 持久状态机 |
| D2 | 不设独立 evidence 插件，证据 provenance 归 `work-registry`（`work.attach_evidence`），gate 是 workflow 读模型 | 独立 `org.vibe.evidence` 插件 |
| D3 | DONE gate 分层：状态机在 work-registry，证据合取式在 workflow 插件；crypto token 推迟到 M2 | M1 就上 review-gate 签发 token |
| D4 | 工作流里程碑事件复用 kernel `event.journal.*`；raw 转录 + per-session canonical 切片归 `session` 插件 | 新建独立 canonical-event 契约 |
| D5 | 契约迁到仓根 `contracts/`，kernel 仓只留 probe fixture（= FIX-5，作为 M1 setup 第一步） | 契约留在 kernel 包 |
| D6 | M1 只接 1 个真实 provider，运行时发现选定 | M1 就做 adapter 接口 + 2 个 provider |
| D7 | 数据模型 §6 一次到位（Task/WorkContext 富字段） | M1 最小字段，M2 再扩 |

## 13. M1 内部里程碑（建议构建顺序）

```text
M1.0  FIX-5 契约分家 + 仓根 contracts/ + Go plugin scaffold 模板 + fixtures/sample-java-project
M1.1  work-registry（Task/WorkContext/状态机/attach_evidence）+ CLI: task create/show
M1.2  workspace-manager（git worktree）
M1.3  agent-adapter：先只 mock provider，agent.run streaming 打通 + AgentRun 持久化
M1.4  artifact-service（collect_diff）+ tool-runner（tool.run + 指纹 + 内容寻址输出）
M1.5  review-gate + session-history（seal/archive/replay 切片）
M1.6  engineering-workflow（无状态编排）+ FIX-4 composition fitness function
M1.7  agent-adapter 接真实 provider #1（运行时发现）
M1.8  Qualification 场景（§10）跑通 + 重启恢复验证 → M1 PASSED
```

每个里程碑：TDD + 致残对照；`scripts/test.sh` 全绿（含 kernel M0.5 回归）；M1 Architecture Gate 人工复核。
