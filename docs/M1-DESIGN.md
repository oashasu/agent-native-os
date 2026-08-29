# M1 — Engineering Vertical Slice 设计

日期：2026-08-29
状态：**基线冻结（Codex CONDITIONAL APPROVAL 的 4 条 blocker + 细化已写回）**
前置：`REVIEW-microkernel-v0.10.0.md`（A2/A3）、`FIX-PLAN`（P0 已完成）、`ADR-001`（Go）
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
| **G1 Kernel Purity** | 没有 `Task / WorkContext / Git / Worktree / Agent / AgentRun / Build / Test / Review / Workflow` 领域语义进入 kernel。 |
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
workflow 进入 WAITING_REVIEW，发 workflow.waiting_review 事件，阻塞等待外部 review.submitted
        │
        │   （另一个终端 / Neovim）
        │   vibe review show <task-id>
        │   vibe review submit <task-id> --decision APPROVED --acceptance AC1=pass ...
        ▼
review.submit ───────────► Review{decision, diff_artifact_id, acceptance_results[], evidence_snapshot}
        │
        ▼
[DONE gate 求值：见 §4]
        │
        ▼
work.complete（IN_REVIEW → DONE，携带 CompletionAssertion）
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

## 4. DONE 不变量 —— M1 就必须不可绕过（D3 blocker）

### 4.1 拆两个能力

```text
work.transition@1     只做非终态转换：
                        PLANNED     → IN_PROGRESS
                        IN_PROGRESS → IN_REVIEW
                        任意        → FAILED
                      （禁跳步 + 乐观版本）

work.complete@1       唯一允许 IN_REVIEW → DONE 的能力
                      输入：CompletionAssertion
                      work-registry 校验 assertion 内部一致性 + 状态 + 版本
```

```text
CompletionAssertion
├── work_context_id
├── diff_artifact_id
├── build_evidence_ref        # EvidenceRef.id
├── test_evidence_ref
├── review_id
├── acceptance_results[]      # {criterion_id, satisfied}
└── issued_by                 # "org.vibe.workflow.engineering"
```

### 4.2 不可绕过靠 Policy 层，不靠 kernel

```text
policy grants:
  org.vibe.workflow.engineering :  work.complete@1   ✓
  local-cli                     :  work.complete@1   ✗   （只有 work.create / work.get / workflow.run / review.submit ...）
  org.vibe.agent.harness        :  work.complete@1   ✗
  其它任何插件                    :  work.complete@1   ✗
```

CLI 想直接 `IN_REVIEW → DONE`？没有 `work.complete@1` grant，kernel 授权层直接拒。
`work.transition@1` 无法表达 `→ DONE`。**没有旁路。**

### 4.3 DONE gate 合取式（由 `engineering-workflow` 求值后构造 CompletionAssertion）

```text
DONE :=
      all(review.acceptance_results[*].satisfied == true)
  AND build EvidenceRef.outcome == PASS
  AND test  EvidenceRef.outcome == PASS
  AND review.decision == APPROVED
  AND review.diff_artifact_id == 当前 agent.run 产出的 diff artifact
  AND review 绑定的是当前 IN_REVIEW attempt（diff 变了旧 review 失效）
```

M2 硬化：`CompletionAssertion` 升级为 review-gate 签发的签名 token。kernel 无关。

## 5. 插件 & 契约地图

契约放 **仓根 `contracts/`**（= FIX-5，M1.0 第一步）。命名 `org.vibe.*` / `capability@major`。

### 5.1 Foundation 能力插件

| 插件 | authority | 契约（kind） | 拥有 | 说明 |
|---|---|---|---|---|
| `org.vibe.blob` | `blob-main` | `blob.put@1`(cmd) `blob.get@1`(query) `blob.stat@1`(query) | bytes ↔ `sha256:` URI | **不理解** Task/Agent/Diff/Session。artifact / tool-runner / agent-adapter / session 都 consume 它，避免隐式共享存储协议。 |
| （kernel 现有）| — | `event.journal.append@1` / `event.journal.replay@1` | canonical 事件链 | 工作流里程碑事件；`workflow.get` 投影源；`session.seal` 事件 selection 源 |

### 5.2 原子领域插件（持有 canonical state）

| 插件 | authority | 契约（kind） | 拥有 |
|---|---|---|---|
| `org.vibe.work.registry` | `work-main` | `work.create@1`(cmd) `work.get@1`(query) `work.transition@1`(cmd) `work.complete@1`(cmd) `work.attach_evidence@1`(cmd) | Task、WorkContext、状态机、`evidence_refs[]` |
| `org.vibe.workspace` | `workspace-main` | `workspace.allocate@1`(cmd) `workspace.release@1`(cmd) `workspace.get@1`(query) | Workspace、git worktree、branch |
| `org.vibe.agent.harness` | `agent-runs-main` | `agent.run@1`(cmd, streaming) `agent.run.get@1`(query) `agent.run.query@1`(query) `agent.run.cancel@1`(cmd) | AgentRun、harness 生命周期归一化 |
| `org.vibe.artifact` | `artifact-main` | `artifact.collect_diff@1`(cmd) `artifact.get@1`(query) `artifact.query@1`(query) | Artifact（diff / 命令输出元数据；内容在 blob） |
| `org.vibe.tool.runner` | `toolruns-main` | `tool.run@1`(cmd) `tool.run.get@1`(query) `tool.run.query@1`(query) | ToolRun、确定性命令执行 + 指纹 |
| `org.vibe.review` | `reviews-main` | `review.submit@1`(cmd) `review.get@1`(query) `review.query@1`(query) | Review 决定、acceptance_results、evidence 快照 |
| `org.vibe.session` | `sessions-main` | `session.seal@1`(cmd) `session.get@1`(query) `session.query@1`(query) | SessionRecord、archive、RecoveryCheckpoint |

`*.query@1`：按 `work_context_id` 查该插件持有的对象列表 —— 支撑 `vibe task show` 的读投影（见 §6）。

### 5.3 Composition 插件（只做编排）

| 插件 | 契约 | 特性 |
|---|---|---|
| `org.vibe.workflow.engineering` | `workflow.engineering.run@1`(cmd, 长 deadline) `workflow.engineering.get@1`(query) | **无私有 canonical state**（见 §7）；`manifest.composition = true`；`consumes` = 上面全部能力 + `event.journal.*`；policy 授予 `work.complete@1` |

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
├── active_workspace_ref      # {id, path, branch, base_commit} | null
├── evidence_refs[]           # EvidenceRef（例外：Work 层审核关系，但只存引用）
└── version

EvidenceRef                   # 不是 evidence 的权威事实源
├── id
├── kind                      # build | test | review
├── source_capability         # "tool.run@1" | "review.submit@1"
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

Review
├── id
├── work_context_id
├── agent_run_id
├── diff_artifact_id          # 绑定：approval 不可跨 diff 复用
├── decision                  # APPROVED | CHANGES_REQUESTED
├── reviewer
├── notes
├── acceptance_results[]      # {criterion_id, satisfied, evidence_refs[], notes}
├── evidence_snapshot[]       # 提交时看到的 {kind, outcome}
└── created_at

SessionRecord
├── id
├── work_context_id
├── agent_run_id
├── archive_ref               # 不可变归档（blob 目录/清单）
├── archive_hash              # 归档内容哈希，seal 后验证
├── event_selection           # 见下
├── recovery_checkpoint       # RecoveryCheckpoint
└── sealed_at

event_selection               # seq range 只是 replay 加速 hint，不是 identity
├── from_seq_hint
├── to_seq_hint
├── trace_id
├── work_context_id
├── agent_run_id
└── （seal 时按 (range hint) AND (trace_id/work_context_id/agent_run_id 匹配) 精确挑选）

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
└── canonical_event_selection # = event_selection
```

**`vibe task show` = 读投影**，不是从 WorkContext 拉数组：

```text
work.get(task_id)
+ workspace.get(active_workspace_ref)
+ agent.run.query(work_context_id)
+ artifact.query(work_context_id)
+ tool.run.query(work_context_id)
+ review.query(work_context_id)
+ session.query(work_context_id)
```

各插件私有 schema（C09）。存储：SQLite/WAL + 增量写（不抄 v0.10 全量 JSON blob）；内容寻址一律走 `org.vibe.blob`。

## 7. 编排模型 —— 解决 A2（D1 有条件通过）

**`engineering-workflow` 是无状态同步编排器。**

- `workflow.engineering.run@1` 一次**同步长 deadline** 命令（默认 30min，可配）。整条链在这一次
  调用内跑完 → kernel 请求作用域 delegation 全程有效。
- 工作流**不持有任何私有 canonical state**。每步向 `event.journal.append` 写事件。
- `workflow.engineering.get@1` = 对 `event.journal.replay@1` 按 `work_context_id` 过滤 + 投影管线进度。
  进度是可重建派生数据（C19）。
- **Human Review 等待模型（D1 条件）**：`IN_REVIEW` 后 workflow 进入 `WAITING_REVIEW`，
  发 `workflow.waiting_review` 事件，**阻塞订阅 `review.submitted` 事件**。review **不由 workflow 自动创建**，
  由人在另一个终端 `vibe review submit`。收到匹配本 work_context 的 `review.submitted` → 继续。
  这顺手验证了 Event Bus + human approval 路径，且不需要 durable state machine。
- **不得把 client transport disconnect 隐式解释成业务 cancel**（D1 条件）。CLI 断连 ≠ 取消 workflow。
  workflow run 是服务端语义，取消必须是显式 `agent.run.cancel` / 未来的 `workflow.cancel`。
  M1：CLI 断连后 workflow 继续跑完（含 WAITING_REVIEW），进度可 `workflow.get` 查回。
- 崩溃语义：run 中途挂 → 各原子插件 state 已按完成步数持久；journal 显示走到哪。**重跑是 M1 人工决定**，无自动 reconciler。
- **不需要 `service_authority`**：`workflow.engineering.run` 由 CLI 身份发起携带 delegation；子调用走 delegation scope。
  CLI 身份 policy grant 列出全部 child 能力（**除** `work.complete@1` —— 那个只授给 workflow 插件本身，见 §4.2）。

  > 注意张力点：`work.complete` 由 workflow 插件调用，而 workflow 是被 CLU delegation 触发的。
  > delegation 链里 principal 仍是 CLI 身份。§4.2 要求 `work.complete` 只有 workflow **插件** 有 grant ——
  > 这依赖 kernel 的 confused-deputy 防护：child 调用需要 **插件自身 grant** AND（principal 直接 grant 或 delegation scope）。
  > 因此 policy 里：workflow 插件有 `work.complete@1` 插件 grant；CLI 的 `workflow.engineering.run@1` delegation scope **包含** `work.complete@1`。
  > CLI 直接调 `work.complete@1`（无 workflow 中转）→ CLI 无该能力 grant → 拒。**验证见 M1.7。**

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

每条 payload 带 `work_context_id`，`Source` 标注产出插件，`trace_id` 贯穿整条 run。

```text
work.created
workspace.allocated
work.transitioned          {from, to}
agent.run.started          {agent_run_id, provider}
agent.run.completed        {agent_run_id, status}
diff.collected             {artifact_id, files_changed, insertions, deletions}
tool.run.completed         {tool_run_id, label, outcome, exit_code}
evidence.attached          {evidence_ref_id, kind, source_id, outcome}
workflow.waiting_review    {work_context_id}
review.submitted           {review_id, decision}
work.completed             {work_context_id}          # ← work.complete@1 产生
session.sealed             {session_id, archive_ref, archive_hash}
workspace.released         {workspace_id, policy}
```

`session.seal` 用 `event.journal.replay@1` + `event_selection` 精确挑选本 run 事件进归档。

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
vibe workflow run <task-id>
        ↓  workspace.allocate (git worktree)
        ↓  agent.run  → 真实 Codex/Claude CLI，实际改文件
        ↓  artifact.collect_diff
        ↓  tool.run build:  ["mvn","-q","-DskipTests","compile"]
        ↓  tool.run test:   ["mvn","-q","test"]
        ↓  work.attach_evidence ×2
        ↓  work.transition(IN_REVIEW) → WAITING_REVIEW（阻塞）

  [另一个终端]
  vibe review show <task-id>
  vibe review submit <task-id> --decision APPROVED --acceptance AC1=pass AC2=pass AC3=pass

        ↓  DONE gate 求值 → work.complete(CompletionAssertion)
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

**外加 G4 独立 qualification（M1.7，真实 provider 之前）**：
```text
# 用 mock 把 Task 推到 IN_REVIEW + 有合法 review
vibe _debug work-transition <task-id> DONE          → 期望：拒（CLI 无 work.transition→DONE 能力）
vibe _debug call work.complete <task-id> --forged   → 期望：拒（CLI 无 work.complete@1 grant）
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
crypto 签名的 CompletionAssertion（M2）
untracked 文件内容打包（M1 只存文件名清单）
```

## 12. D1–D7 最终结论（Codex 评审后）

| # | 结论 | 锁定版本 |
|---|---|---|
| D1 | **有条件通过** | 同步长调用 + journal 投影；明确 `WAITING_REVIEW → 外部 review.submit → 继续`；client disconnect 不等于 business cancel |
| D2 | **通过，改数据结构** | 不设 evidence 插件；work-registry 存 `EvidenceRef`（引用 + 观测哈希 + invalidated_at），事实仍在 ToolRun/Review |
| D3 | **不通过 → M1 blocker，已解决** | 新增 `work.complete@1`，policy 只授 workflow 插件；`work.transition@1` 无法表达 →DONE；M1.7 独立 qualification |
| D4 | **通过，修正 seal 语义** | `event_selection` = seq hint AND (trace_id/work_context_id/agent_run_id 匹配)；新增 `RecoveryCheckpoint`；`workspace.release` 只在 seal 验证后 |
| D5 | **通过** | contracts 迁仓根，M1.0 直接做 |
| D6 | **通过** | real #1 + mock fixture 长期保留 |
| D7 | **方向通过，模型调整** | Task/WorkContext/AgentRun 现在就建；WorkContext 删跨插件 canonical ID 数组；acceptance_criteria 拆成 Task{id,text} + Review.acceptance_results[] |

**已冻结、不再讨论**：A 薄而完整竖切、真实 Harness + mock 双轨、无状态 composition、一个 Provider、
contracts 仓根化、engineering 语义不回流 kernel。

## 13. M1 内部里程碑（Codex 重排：DONE 不可绕过在真实 provider 之前）

```text
M1.0  FIX-5 契约分家 + 仓根 contracts/ + Go plugin scaffold 模板 + fixtures/sample-java-project
M1.1  org.vibe.blob（blob.put/get/stat）+ work-registry（Task/WorkContext/状态机/complete/attach_evidence）
      + CLI: vibe task create / show
M1.2  workspace-manager（git worktree）
M1.3  agent-adapter：只 mock provider，agent.run streaming 打通 + AgentRun 持久化
M1.4  artifact-service（collect_diff）+ tool-runner（结构化 argv + 指纹 + blob 输出）
M1.5  review-gate（acceptance_results）+ session-history（seal/archive/replay selection/RecoveryCheckpoint）
M1.6  engineering-workflow（无状态编排 + WAITING_REVIEW + Human review）+ FIX-4 composition fitness function
M1.7  G4 独立 qualification：DONE 不可绕过（mock 链 + 伪造调用被拒）
M1.8  agent-adapter 接真实 provider #1（运行时发现）
M1.9  完整 qualification（§10）+ kill runtime + restart kernel + recovery 验证 → G1–G6 全过 → M1 PASSED
```

每个里程碑：TDD + 致残对照；`scripts/test.sh` 全绿（含 kernel M0.5 回归）；G1 人工复核。
