# M1 — Engineering Vertical Slice 设计

日期：2026-08-29
状态：**基线冻结（Codex CONDITIONAL APPROVAL 的 4 条 blocker + 细化已写回）+ ADR-002 交互模型对齐（2026-08-29：契约面零改动；净影响 = §10 增一条可证伪的 Console 读投影验收 + §11 scope 澄清）**
前置：`REVIEW-microkernel-v0.10.0.md`（A2/A3）、`FIX-PLAN`（P0 已完成）、`ADR-001`（Go）、`ADR-002`（Human Console 交互模型 —— 影响 §5.2 / §6 / §11 / §13）
方向输入：项目所有者 + Codex（Q1=A / Q2=Real Harness + mock 双轨 / M1 Architecture Gate / 条件批准）

---

## 1. M1 要证明的唯一命题

> **一条真实工程工作流可以完全建立在 kernel 的 capability / plugin contract 之上，
> 不需要任何 engineering-specific 语义回流 Microkernel。**

不证明"AEOS 完整领域模型已迁移正确"（M2+）。不证明"GitHub/CI/Release 打通"（M3）。
只证明骨架成立：真实 Agent 通过外部能力链完成一次真实工程变更；`Agent completed ≠ Task DONE`
这个领域不变量被真正强制；Runtime 消失后状态可恢复。

## 2. M1 完成判据 —— 6 条 Gate（取代"happy-path 全绿"）

| Gate | 判据 |
|---|---|
| **G1 Kernel Purity** | 没有工程领域语义进入 kernel/protocol 公开类型/函数名。`check_boundaries.py` 词汇护栏当前禁：`Task Session Agent Workflow Git Knowledge Spring JPA Weather Learning ReviewGate WorkContext`。M1 期间不放宽该列表，且不动 kernel 源（README 除外）。 |
| **G2 Real Execution** | 至少一个真实 Harness 实际修改真实 git worktree。 |
| **G3 Truth Chain** | Diff + Build + Test + Acceptance + Review 全部形成可追溯事实链（reference 而非复制）。 |
| **G4 DONE Integrity** | 无法绕过 gate 直接把 Task 置 DONE（见 §4）。**M1.7 单独 qualification 一次。** |
| **G5 Persistence** | kill agent + restart kernel 后所有 canonical objects 和 raw history 仍可找到。 |
| **G6 Recovery** | 即使原 Agent Runtime 已不存在，`RecoveryCheckpoint` 足以继续工作。 |

**G1 的执行方式：**
- `architecture-tests/check_boundaries.py` kernel 词汇黑名单已禁上述词，M1 期间不放宽。
- FIX-4 composition fitness function 随 `engineering-workflow` 插件落地。
- 每个里程碑结束人工复核 kernel `git diff` 只动 mechanism。
- kernel 在 M1 期间**只接受 FIX-PLAN 的 P1/P2 硬化项**，不接受任何新领域概念。

## 3. 锁定的工作流链（线性，无 stage machine）

```text
Task 规格（goal + scope + acceptance_criteria[{id,text}]）
        │
        ▼
work.create ──────────────► Task + WorkContext（status=PLANNED）
        │
        ▼
workspace.allocate ───────► git worktree + branch（base_commit 记录）
        │
        ▼
work.transition(PLANNED → IN_PROGRESS)
        │
        ▼
agent.run（真实 Harness，streaming）
        │   ├── source changes（写进 worktree）
        │   ├── agent.frame stream（stdout/stderr/status）
        │   └── raw_session_ref（原始转录，经 blob.put 落内容寻址）
        ▼
artifact.collect_diff ────► Artifact{kind=diff, blob_uri, summary}
        │
        ▼
tool.run(label=build)  ["mvn","-q","-DskipTests","compile"]  ──► ToolRun → outcome
        │
        ▼
tool.run(label=test)   ["mvn","-q","test"]                    ──► ToolRun → outcome
        │
        ▼
work.attach_evidence ×N ──► WorkContext.evidence_refs[] += EvidenceRef
        │
        ▼
work.transition(IN_PROGRESS → IN_REVIEW)
        │
        ▼
review.request ──────────► Review{status=PENDING, diff_artifact_id, evidence_snapshot}
        │
        ▼
workflow 进入 WAITING_REVIEW，发 workflow.waiting_review 事件，退避轮询 review.get(review_id)
        │
        │   （另一个终端 / Neovim）
        │   vibe review show <task-id>
        │   vibe review decide <review-id> --approved --acceptance AC1=pass ...
        ▼
review.get 返回 status=APPROVED（+ acceptance_results[]）
        │
        ▼
[DONE gate 求值：见 §4.3]
        │
        ▼
delegated work.transition(IN_REVIEW → DONE, expected_version=N)   # 仅 workflow 经 delegation scope 可调
        │
        ▼
session.seal ────────────► SessionRecord + 不可变 archive（raw + canonical 事件 selection + RecoveryCheckpoint）
        │
        ▼
验证 archive hash / checkpoint 完整
        │
        ▼
agent.run 的 Runtime 终止
        │
        ▼
workspace.release(policy=preserve)    # M1：不物理删 worktree
```

每步由 `engineering-workflow` composition 插件驱动，并向 `event.journal.append` 写一条 canonical 事件（§9）。

## 4. DONE 不变量 —— M1 就必须不可绕过（D3，靠既有 policy/delegation，不新增契约）

Codex 复核实际 authz + delegation 实现后：**不需要 `work.complete@1`，也不需要 crypto token。**
既有 kernel 已提供足够机制（`policy.json` 的 `workflow-user` demo 就是同一模式）。

### 4.1 单一转换契约 + 真实状态机（M1.1 实现）

```text
work.transition@1     输入：{work_context_id, to, expected_version(必填)}
                      合法转换：
                        PLANNED     → IN_PROGRESS
                        IN_PROGRESS → IN_REVIEW
                        IN_REVIEW   → DONE
                        任意非 DONE  → FAILED
                      非法跳转（如 PLANNED → DONE、IN_PROGRESS → DONE）→ 拒
                      expected_version 不匹配 → CONFLICT
```

（当前 v0.10 work-registry 的 `w.Status = q.To` 无状态机、`expected_version` 可选 —— M1.1 正式重做，不带 demo 兼容包袱。）

### 4.2 不可绕过靠 Policy + Delegation

```text
qualification 用户（外部身份）policy grant：
  capabilities:  work.create@1, work.get@1, workflow.engineering.run@1, review.request@1, review.decide@1, review.get@1
                 （不含 work.transition@1）
  delegations:
    workflow.engineering.run@1 → [ work.create@1, work.transition@1, workspace.*@1, agent.run@1,
                                   tool.run@1, artifact.*@1, review.request@1, review.get@1,
                                   session.seal@1, event.journal.append@1, blob.put@1 ]

org.vibe.workflow.engineering  插件 grant：  consume/capabilities 含 work.transition@1
```

结果：

```text
vibe work transition <task> DONE           → DENIED（外部身份无 work.transition@1 grant）
vibe workflow run <task>                    → workflow 求值 DONE gate → delegated work.transition(DONE) → ALLOWED
```

外部身份能调 `workflow.engineering.run`，但 `work.transition` 只能经 workflow 中转（delegation scope 授予）。
workflow 是唯一 gate keeper，且它调 `work.transition(DONE)` 前必须先过 §4.3。

### 4.3 DONE gate 合取式（`engineering-workflow` 求值后才调 `work.transition(to=DONE)`）

```text
DONE :=
      all(review.acceptance_results[*].satisfied == true)
  AND build EvidenceRef.outcome == PASS
  AND test  EvidenceRef.outcome == PASS
  AND review.status == APPROVED
  AND review.diff_artifact_id == 当前 agent.run 产出的 diff artifact
  AND review 绑定的是当前 IN_REVIEW attempt（diff 变了旧 review 失效）
```

### 4.4 M1.7 对抗 qualification（真实 provider 之前）

```text
external direct  work.transition(<task>, DONE)       → 期望 DENIED（无 capability grant）
stale review：diff 变更后用旧 review 走 DONE          → 期望拒（diff_artifact_id 不匹配 / invalidated_at）
failed test：test EvidenceRef.outcome == FAIL 时 DONE → 期望拒（gate 合取式）
wrong-diff approval：review 指向别的 diff artifact    → 期望拒
仅 vibe workflow 正常路径能把 Task 置 DONE
```

M2 硬化：review-gate 签发 capability token；`work.transition(DONE)` 要求携带。kernel 无关。

## 5. 插件 & 契约地图

契约放 **仓根 `contracts/`**（= FIX-5，M1.0 第一步）。命名 `org.vibe.*` / `capability@major`。

### 5.1 Foundation 能力插件

| 插件 | authority | 契约（kind） | 拥有 | 说明 |
|---|---|---|---|---|
| `org.vibe.blob` | `blob-main` | `blob.put@1`(cmd) `blob.get@1`(query) `blob.stat@1`(query) | bytes ↔ `sha256:` URI | **不理解** Task/Agent/Diff/Session。artifact / tool-runner / agent-adapter / session 都 consume 它，避免隐式共享存储协议。 |
| `org.vibe.event.journal`（port 自 kernel） | `journal-main` | `event.journal.append@1`(cmd) / `event.journal.replay@1`(query) | **durable hash-chain journal，非 pub/sub**。append 返回 `record.id` + `record.sha256`。 | 里程碑事件；`session.seal` 的事件 selection；`workflow.get` 客户端侧按 `correlation_id` 过滤投影 |

> **event.journal 不是事件总线。** append 是写日志文件 + fsync + hash chain；replay 是文件扫描（`after`/`limit` 是隐式行号，**没有 seq 字段**）。任何"等某事件发生"的语义靠**调用方轮询 replay/get**，不是订阅。Foundation journal **不得**增加 `work_context_id` 过滤（否则 Foundation 开始理解工程领域）；通用过滤（trace_id/correlation_id/type/source）留给 M2/M3。

### 5.2 原子领域插件（持有 canonical state）

| 插件 | authority | 契约（kind） | 拥有 |
|---|---|---|---|
| `org.vibe.work.registry` | `work-main` | `work.create@1`(cmd) `work.get@1`(query) `work.transition@1`(cmd) `work.attach_evidence@1`(cmd) | Task、WorkContext、**真实状态机**（§4.1）、`evidence_refs[]` |
| `org.vibe.workspace` | `workspace-main` | `workspace.allocate@1`(cmd) `workspace.release@1`(cmd) `workspace.get@1`(query) | Workspace、git worktree、branch |
| `org.vibe.agent.harness` | `agent-runs-main` | `agent.run@1`(cmd, streaming) `agent.run.get@1`(query) `agent.run.query@1`(query) `agent.run.cancel@1`(cmd) | AgentRun、harness 生命周期归一化 |
| `org.vibe.artifact` | `artifact-main` | `artifact.collect_diff@1`(cmd) `artifact.get@1`(query) `artifact.query@1`(query) | Artifact（diff / 命令输出元数据；内容在 blob） |
| `org.vibe.tool.runner` | `toolruns-main` | `tool.run@1`(cmd) `tool.run.get@1`(query) `tool.run.query@1`(query) | ToolRun、确定性命令执行 + 指纹 |
| `org.vibe.review` | `reviews-main` | `review.request@1`(cmd) `review.decide@1`(cmd) `review.get@1`(query) `review.query@1`(query) | Review（PENDING→APPROVED/CHANGES_REQUESTED）、acceptance_results、evidence 快照 |
| `org.vibe.session` | `sessions-main` | `session.seal@1`(cmd) `session.get@1`(query) `session.query@1`(query) | SessionRecord、archive、RecoveryCheckpoint |

`*.query@1`：按 `work_context_id` 查该插件持有的对象列表 —— 支撑 `vibe task show` 的读投影（见 §6）。

**上下文枚举（`work.query@1`）不在 M1**（ADR-002）：Console 的上下文切换器要"列出所有活动 WorkContext"，当前只有 `work.get(task_id)`。但 **M1 里没有任何消费方** —— §10 是单任务 qualification，smoke 不用，workflow 不用。按主路径优先，它归 UI 阶段（见 §11），不塞进 M1 的关键路径。
ADR-002 对 M1 的真实影响不是这个功能，而是 §10 的 **Console 读投影充分性验收** —— 那条验收才会证伪"读投影已足够支撑两个镜头"这个判断。

### 5.3 Composition 插件（只做编排）

| 插件 | 契约 | 特性 |
|---|---|---|
| `org.vibe.workflow.engineering` | `workflow.engineering.run@1`(cmd, 默认 30min deadline) `workflow.engineering.get@1`(query) | **无私有 canonical state**（见 §7）；manifest JSON 里 `"composition": true`（**仅 repo-level 元数据，不进 kernel Manifest struct**，由 `check_composition.py` 读取）；`consumes` = §5.2 全部能力 + `event.journal.*` + `blob.put@1`；plugin grant 含 `work.transition@1` |

## 6. 数据模型

```text
Task
├── id
├── title
├── goal
├── scope
├── acceptance_criteria[]     # {id, text}          ← 纯需求定义，不含 satisfied
├── status                    # PLANNED|IN_PROGRESS|IN_REVIEW|DONE|FAILED
├── version
└── work_context_id

WorkContext                   # 瘦身：不再持有跨插件 canonical ID 数组（避免 distributed double-write）
├── id
├── task_id
├── repo                      # 本地仓库路径（M1）
├── active_workspace_ref      # M1.1 落成 always-null 保留字段；WC↔workspace 关系是
│                             # WorkspaceRef.work_context_id，由 workspace.get{work_context_id} 回答，
│                             # 不写回 WorkContext（与"无 mirror ID"一致，M1.2 定）
├── evidence_refs[]           # EvidenceRef（例外：Work 层审核关系，但只存引用）
└── version

EvidenceRef                   # 不是 evidence 的权威事实源
├── id
├── kind                      # build | test | review
├── source_capability         # "tool.run@1" | "review.decide@1"
├── source_id                 # ToolRun.id | Review.id
├── outcome                   # PASS | FAIL
├── observed_at
├── content_hash              # 观测时 source 对象的哈希
└── invalidated_at?           # diff 变更后置此，旧 evidence 失效

AgentRun
├── id
├── work_context_id           # ← 唯一关联方式
├── workspace_ref
├── prompt
├── provider                  # "mock" | "<discovered real provider>"
├── harness_native_id         # provider 私有会话标识（external metadata，C32）
├── status                    # RUNNING|COMPLETED|FAILED|CANCELLED|TIMEOUT
├── raw_session_ref           # blob URI（原始转录）
├── provider_metadata         # provider 私有字段 blob
├── started_at / ended_at

Artifact
├── id
├── work_context_id
├── kind                      # "diff" | "command_output"
├── blob_uri                  # sha256: 内容寻址
├── summary                   # diff: {files_changed, insertions, deletions, files[]}
└── created_at

ToolRun
├── id
├── work_context_id
├── workspace_ref
├── label                     # "build" | "test"
├── command[]                 # 结构化 argv，不接受 shell 字符串
├── cwd                       # = workspace path
├── env_allowlist[]           # 只透传白名单环境变量
├── timeout_ms
├── exit_code
├── outcome                   # PASS(exit 0) | FAIL
├── stdout_uri / stderr_uri   # blob URI
├── fingerprint               # hash(command + env_allowlist值 + workspace.base_commit)
├── started_at / ended_at

Review                        # review.request 创建（status=PENDING）；review.decide 落定
├── id
├── work_context_id
├── agent_run_id
├── diff_artifact_id          # 绑定：approval 不可跨 diff 复用
├── status                    # PENDING | APPROVED | CHANGES_REQUESTED
├── reviewer                  # review.decide 时填
├── notes
├── acceptance_results[]      # {criterion_id, satisfied, evidence_refs[], notes}  ← review.decide 时填
├── evidence_snapshot[]       # review.request 时看到的 {kind, outcome, evidence_ref_id}
├── requested_at
└── decided_at?

SessionRecord
├── id
├── work_context_id
├── agent_run_id
├── archive_ref               # 不可变归档（blob 目录/清单）
├── archive_hash              # 归档内容哈希，seal 后验证
├── event_selection           # SessionEventSelection（见下）
├── recovery_checkpoint       # RecoveryCheckpoint
└── sealed_at

SessionEventSelection         # Event Journal 没有 seq；不假设它有
├── journal_cursor_start      # append 前对 replay 取的 next 值（加速 hint，非 identity）
├── journal_cursor_end        # seal 时 replay 的 next 值
├── correlation_id            # = work_context_id（本 run 所有事件都带）
├── event_ids[]               # 每次 event.journal.append 返回的 record.id —— 精确身份
├── event_sha256s[]           # 对应 record.sha256 —— seal 时 + 归档校验时复核
└── event_count

RecoveryCheckpoint
├── repo
├── base_commit
├── head_commit
├── branch
├── worktree_path_at_seal
├── dirty                     # bool
├── tracked_patch_ref         # blob URI（tracked + staged diff）
├── untracked_manifest[]      # 文件名列表（M1 不打包 untracked 内容）
├── diff_artifact_id
├── task_id
├── work_context_id
├── agent_run_id
├── provider
├── harness_native_id?
└── canonical_event_selection # = SessionEventSelection
```

**`vibe task show` = 读投影**，不是从 WorkContext 拉数组：

```text
work.get(task_id)
+ workspace.get({work_context_id})
+ agent.run.query(work_context_id)
+ artifact.query(work_context_id)
+ tool.run.query(work_context_id)
+ review.query(work_context_id)
+ session.query(work_context_id)
```

各插件私有 schema（C09）。存储：SQLite/WAL + 增量写（不抄 v0.10 全量 JSON blob）；内容寻址一律走 `org.vibe.blob`。

**Console 的"本次改动" = diff Artifact 的 `summary.files[]`**（ADR-002）：IDE 工作台与 Agent 工作台渲染的是同一份多文件清单。**人手动编辑 worktree 与 agent 编辑等价** —— 都会让 `artifact.collect_diff` 下一次产出不同的 diff artifact，触发 `EvidenceRef.invalidated_at`、旧 Review 按 §4.3 失效。IDE 工作台在 `workspace.get` 给出的真实本地 worktree 路径上直接操作文件系统与本地 `git`，不经 kernel 契约；kernel 只经上述读投影提供围绕 worktree 的工作上下文元数据。

## 7. 编排模型 —— 解决 A2（D1 有条件通过）

**`engineering-workflow` 是无状态同步编排器。**

- `workflow.engineering.run@1` 一次**同步长 deadline** 命令（默认 30min，可配）。整条链在这一次
  调用内跑完 → kernel 请求作用域 delegation 全程有效。
- 工作流**不持有任何私有 canonical state**。每步向 `event.journal.append` 写事件。
- 本 run 所有 canonical 事件带 `correlation_id = <work_context_id>`。
- `workflow.engineering.get@1` = 调 `event.journal.replay@1`（全量分页），在 **workflow 插件侧**按
  `correlation_id` 过滤 + 投影管线进度。进度是可重建派生数据（C19）。Foundation journal 不加领域过滤。
- **Human Review 等待模型（D1 条件，轮询而非订阅）**：event.journal **不是 pub/sub**，不能"等事件"。
  流程：
  ```text
  workflow  →  review.request  →  Review{status=PENDING, diff_artifact_id, evidence_snapshot}
            →  发 workflow.waiting_review 事件（信息用途）
            →  WAITING_REVIEW：周期 review.get(review_id)，退避轮询
                                          ↑
                        另一个终端： vibe review decide <review-id> --approved --acceptance AC1=pass ...
                                          → Review{status=APPROVED, acceptance_results, decided_at}
            ←  review.get 返回 status != PENDING  →  继续 DONE gate
  ```
  review **不由 workflow 自动创建决定**。M1 不需要 durable workflow engine。
  M2 升级为 `review.decided` 事件订阅 + background reconciler + 持久 workflow state。
- **不得把 client transport disconnect 隐式解释成业务 cancel**（D1 条件）。CLI 断连 ≠ 取消 workflow。
  取消必须是显式 `agent.run.cancel` / 未来的 `workflow.cancel`。M1：CLI 断连后 workflow 继续跑完
  （含 WAITING_REVIEW 轮询），进度可 `workflow.engineering.get` 查回。
- **CLI 超时**：kernel 的 `vibe` 默认 `-timeout 10s`。M1 产品 CLI（M1.1+）的 `vibe workflow run`
  默认 deadline 30min（`--timeout` 可覆盖）。`workflow.engineering.run@1` 契约不设固定超时，由调用方 deadline 决定。
- 崩溃语义：run 中途挂 → 各原子插件 state 已按完成步数持久；journal 显示走到哪。**重跑是 M1 人工决定**，无自动 reconciler。
- **不需要 `service_authority`**：`workflow.engineering.run` 由外部身份发起携带 delegation；子调用走 delegation scope。
  外部身份 policy grant **不含** `work.transition@1`；只有 workflow 插件自身 consume/grant 了它，
  且外部身份对 `workflow.engineering.run@1` 的 delegation scope **包含** `work.transition@1`（见 §4.2）。
  这依赖 kernel confused-deputy 防护：child 调用需 **插件自身 grant** AND（principal 直接 grant 或 delegation scope）。
  外部身份直接调 `work.transition@1`（无 workflow 中转）→ 无 capability grant → 拒。**验证见 M1.7。**

**M2 硬化**：改 `service_authority` + 持久编排状态机 + reconciler，支持自动续跑 / rework 回环。

## 8. 真实 Harness Adapter（方向已批准，不改）

- **Provider 中立**：`agent.run@1` 契约不得出现 `CodexThread / ClaudeConversation`。只认
  `prompt` / `workspace_ref` / `provider?` / 返回 `AgentRun` + `agent.frame` stream。
  私有 → `provider_metadata` / `harness_native_id` / `raw_session_ref`。
- **运行时发现**：agent-adapter 启动探测 `codex --version` / `claude --version` / ...，选可用且接口最稳的为 real #1。
  **M1 只接一个真实 provider。** 这台开发机可用 CLI 在 M1.8 前用 `which` / `--version` 确认。
- **双轨（长期结构，非临时）**：

```text
                  agent.run@1 契约
             ┌──────────┴──────────┐
        mock provider          real provider #1
        （长期保留）            （qualification）
        确定性 CI regression    codex / claude CLI
        + 故障注入：            端到端真实变更
          completion/failure/timeout/
          approval-required/malformed-event/
          crash/restart/partial-output/cancellation
```

`mock-agent` 从 v0.10 迁入并长期保留。`scripts/test.sh` 用 mock 跑 regression；§10 qualification 用 real。

## 9. Canonical 事件清单（`event.journal.append`）

每条 payload 带 `work_context_id`；`correlation_id = work_context_id`；`Source` 标注产出插件；`trace_id` 贯穿整条 run。
workflow 累积每次 append 返回的 `record.id` + `record.sha256`，最后交给 `session.seal`（`SessionEventSelection`）。

```text
work.created
workspace.allocated
work.transitioned          {from, to}
agent.run.started          {agent_run_id, provider}
agent.run.completed        {agent_run_id, status}
diff.collected             {artifact_id, files_changed, insertions, deletions}
tool.run.completed         {tool_run_id, label, outcome, exit_code}
evidence.attached          {evidence_ref_id, kind, source_id, outcome}
workflow.waiting_review    {work_context_id, review_id}
review.requested           {review_id, diff_artifact_id}
review.decided             {review_id, status}
session.sealed             {session_id, archive_ref, archive_hash}
workspace.released         {workspace_id, policy}
```

（`work.transitioned {to: DONE}` 就是 DONE 事件，不再单列 `work.completed`。）

`session.seal` 按 `SessionEventSelection.event_ids[]`（精确身份）+ `journal_cursor` 提示 从 `event.journal.replay@1`
挑出本 run 事件进归档，并逐条比对 `event_sha256s[]`。

## 10. Qualification 场景（G1–G6 全过 → "M1 ENGINEERING VERTICAL SLICE: PASSED"）

`fixtures/sample-java-project/`（Maven，含 `Calculator.add` + 现有测试）。

Task：
```text
goal: 修改 Calculator.add，对非法输入（null / 溢出）增加指定行为，并补充测试。
scope: 只允许改 Calculator.java 与其测试文件。
acceptance_criteria:
  AC1: mvn -q test PASS
  AC2: 新增测试覆盖非法输入行为
  AC3: 未改动无关文件
```

```text
vibe task create --goal ... --scope ... --acceptance AC1=... AC2=... AC3=... --repo fixtures/sample-java-project
vibe workflow run <task-id> --timeout 30m
        ↓  workspace.allocate (git worktree)
        ↓  work.transition(IN_PROGRESS)
        ↓  agent.run  → 真实 Codex/Claude CLI，实际改文件
        ↓  artifact.collect_diff
        ↓  tool.run build:  ["mvn","-q","-DskipTests","compile"]
        ↓  tool.run test:   ["mvn","-q","test"]
        ↓  work.attach_evidence ×2
        ↓  work.transition(IN_REVIEW)
        ↓  review.request → WAITING_REVIEW（轮询 review.get）

  [另一个终端]
  vibe review show <task-id>
  vibe review decide <review-id> --approved --acceptance AC1=pass AC2=pass AC3=pass

        ↓  review.get 返回 APPROVED → DONE gate 求值（§4.3）
        ↓  delegated work.transition(to=DONE, expected_version=N)
        ↓  session.seal → 验证 archive_hash / RecoveryCheckpoint
        ↓  agent runtime terminate
        ↓  workspace.release(policy=preserve)
```

**关键动作**：
```text
杀掉 agent runtime + 关闭 CLI client
重启 kernel
vibe task show <task-id>
```
仍完整可见：`goal / acceptance_criteria / agent_run / worktree(path,base_commit,head_commit) /
diff / build evidence / test evidence / review(+acceptance_results) / session_record / RecoveryCheckpoint / raw_history_ref`。

**外加 Console 读投影充分性验收（M1.9，ADR-002）**：

ADR-002 断言"既有读投影已足够支撑 IDE / Agent 两个镜头，无需返工"。这条断言**必须可证伪**，
否则要到 UI 阶段才会暴露 —— 正是当初担心的"晚发现、返工大"。做法：重启 kernel 后，**只用
Console 会用的那组读投影**重建一次任务视图，逐字段断言。

```text
work.get(task_id) + workspace.get{work_context_id} + agent.run.query + artifact.query
+ tool.run.query + review.query + session.query          # 不得读任何插件私有存储

IDE 镜头必需字段：
  workspace.path            # 目录树 / 编辑器的根
  workspace.base_commit     # git 历史起点
  RecoveryCheckpoint.head_commit / .branch / .dirty
  Artifact{kind=diff}.summary.files[]                    # 多文件 changeset
Agent 镜头必需字段：
  AgentRun{id, provider, status, raw_session_ref}        # 会话转录入口
  EvidenceRef[]{kind, outcome, source_id}                # 证据链
  Review{status, diff_artifact_id, acceptance_results[]} # 完成闸 + 裁决
  SessionRecord{archive_ref, archive_hash}               # 已封存会话

任一字段缺失 / 拿不到 → 验收 FAIL → ADR-002 的"无需返工"结论被证伪，当场返工。
```

**致残对照**：把 `Artifact.summary.files[]` 置空，该验收必须变红；不变红说明它在空转。

**外加 G4 独立 qualification（M1.7，真实 provider 之前）** —— 见 §4.4：
```text
external direct  work.transition(<task>, DONE)   → 拒（外部身份无 work.transition@1 capability grant）
stale review / failed test / wrong-diff approval → 拒（§4.3 gate 合取式）
仅 vibe workflow 正常路径能把 Task 置 DONE
```

## 11. NON-GOALS

```
完整 ANALYSIS/DESIGN/PLAN stage machine 与 rework 回环
Semantic Graph / Spring why-bean / JPA 语义模型
GitHub PR / webhook / CI connector / Release 验证
Knowledge Promotion / Vector Search
Desktop UI / 完整 Neovim UI
多 Agent Team / 多 Provider 生产级支持
自动 workflow reconciler / 自动续跑 / rework 回环
分布式 / 多 host authority
crypto / capability-token 签名的 review 授权（M2）
review.decided 事件订阅 + background reconciler（M2）
Foundation journal 的领域/通用过滤（M2/M3）
untracked 文件内容打包（M1 只存文件名清单）
新增 work.complete@1 契约（**撤回** —— 用既有 policy/delegation，见 §4）
Console 上下文枚举 work.query@1 / vibe work list（UI 阶段 —— M1 无消费方，见 §5.2；ADR-002）
Console 读模型（切换器一行要 agent/分支/时间 → 当前是 N+1 次查询；大规模导航 G9 需要读模型插件，M2；ADR-002）
Agent 交互式追问 / 向运行中的 agent.run 追加消息（M2 —— M1 agent.run 一次性 streaming；ADR-002）
结构化转录卡片渲染（依赖 provider adapter 归一化 —— M1.8+ / UI 阶段；M1 只保证 raw_session_ref + agent.frame 流；ADR-002）
本地 git 提交历史可视化（UI 阶段 —— worktree 是本地目录，IDE 工作台直接跑 git；ADR-002）
IDE / Agent 双 lens 前端本体（UI 阶段 —— Neovim，ADR-012；本设计只保证读投影支持，ADR-002）
```

## 12. D1–D7 最终结论（Codex 两轮评审后）

| # | 结论 | 锁定版本 |
|---|---|---|
| D1 | **有条件通过** | 同步长调用 + journal 投影；`WAITING_REVIEW` 靠 **轮询 `review.get`**（event.journal 非 pub/sub）；client disconnect ≠ business cancel；CLI workflow 默认 30min |
| D2 | **通过，改数据结构** | 不设 evidence 插件；work-registry 存 `EvidenceRef`（引用 + 观测哈希 + invalidated_at），事实仍在 ToolRun/Review |
| D3 | **不新增契约（撤回 work.complete）** | 用既有 policy + delegation：外部身份无 `work.transition@1` grant，只经 workflow delegation scope 中转；`work.transition` M1.1 实真实状态机 + `expected_version` 必填；M1.7 对抗 qualification |
| D4 | **通过，修正 seal 语义** | Event Journal **无 seq**；`SessionEventSelection` = `event_ids[]` + `event_sha256s[]`（精确身份）+ journal_cursor 提示 + `correlation_id`；新增 `RecoveryCheckpoint`；`workspace.release` 只在 seal 验证后；journal 不加 `work_context_id` 过滤 |
| D5 | **通过** | contracts 迁仓根（M1.0）；kernel/contracts/ 重定性为 kernel 自测 fixture，物理不动 kernel 源 |
| D6 | **通过** | real #1 + mock fixture 长期保留 |
| D7 | **方向通过，模型调整** | Task/WorkContext/AgentRun 现在就建；WorkContext 只留 `{id,task_id,repo,active_workspace_ref,version}` + `evidence_refs[]`，删所有 mirror ID 数组；acceptance_criteria 拆成 Task{id,text} + Review.acceptance_results[] |

**已冻结、不再讨论**：A 薄而完整竖切、真实 Harness + mock 双轨、无状态 composition、一个 Provider、
contracts 仓根化、engineering 语义不回流 kernel（且已有 `check_boundaries.py` 词汇护栏机器强制）。
`composition: true` 是 manifest JSON 里的 repo-level 元数据，**不进** kernel Manifest Go struct，由 `check_composition.py` 读取。

## 13. M1 内部里程碑（Codex 重排：DONE 不可绕过在真实 provider 之前）

```text
M1.0  contracts 分家 + composition fitness (check_composition.py) + go.work + event.journal foundation 插件 — done 000544f
      + plugin scaffold 模板 + fixtures/sample-java-project + M1 部署 config/smoke
M1.1  org.vibe.blob（blob.put/get/stat）+ work-registry（Task/WorkContext/真实状态机/attach_evidence/
      expected_version 必填）+ 产品 CLI: vibe task create / show / transition — done (PR #2)
M1.2  workspace-manager（git worktree） — done (PR #3)
M1.3  agent-adapter：只 mock provider，agent.run streaming 打通 + AgentRun 持久化 — done (PR #4)
M1.4  artifact-service（collect_diff）+ tool-runner（结构化 argv + 指纹 + blob 输出） — done (PR #5)
M1.5  review（request/decide/get）+ session-history（seal/archive/SessionEventSelection/RecoveryCheckpoint） — done (PR #6)
M1.6  engineering-workflow（无状态编排 + WAITING_REVIEW 轮询 + DONE gate）
M1.7  对抗 qualification：external direct DONE 被拒 / stale review 被拒 / failed test 不能 DONE / wrong-diff approval 不能 DONE
M1.8  agent-adapter 接真实 provider #1（运行时发现 codex/claude）
M1.9  完整 qualification（§10）+ kill runtime + restart kernel + recovery 验证
      + Console 读投影充分性验收（§10，证伪 ADR-002 的"无需返工"结论）→ G1–G6 全过 → M1 PASSED
```

每个里程碑：TDD + 致残对照；`scripts/test.sh` 全绿（含 kernel M0.5 回归）；G1 `check_boundaries.py` + 人工复核。
