# 微内核修复计划 — v0.10.0 → v0.11.0

配套：`REVIEW-microkernel-v0.10.0.md`
原则（用户级工作纪律）：主路径优先；边角情形登记为观察 + 继续；致残对照验收。
每个修复项：**变更点 → 验收（含致残对照）→ 影响面**。

## 状态

| 批次 | 项 | 状态 | commit |
|---|---|---|---|
| P0 | FIX-1 内核启动韧性 | ✅ 完成（TDD + 致残 + E2E smoke） | `6d78bbf` |
| P0 | FIX-2 注册表回收死亡 runtime | ✅ 完成（TDD + 致残 x2） | `b3c1be6` |
| P0 | FIX-3 写超时 / 事件扇出隔离 / stream 段隔离 | ✅ 完成（TDD + 致残 x3，`-race` 干净） | `d83e67c` |
| P1 | FIX-4 composition fitness function | ⏳ **M1.0 落 `check_composition.py` seed**（M1.6 绑 engineering-workflow） | 见 `plans/2026-08-29-m1-0-scaffold.md` Task 7 |
| P1 | FIX-5 契约与内核分家 | ⏳ **M1.0**（重定性：kernel/contracts/ 为 kernel 自测 fixture，物理不动 kernel 源；产品契约进仓根 contracts/） | 见 M1.0 plan Task 2 |
| P2 | FIX-6 ~ FIX-15 硬化项 | ⏳ 按需穿插 |  |

P0 全部落地后 `bash scripts/test.sh` 全绿，含 M0.5 ADVERSARIAL QUALIFICATION: PASSED。
竖切设计已冻结（`M1-DESIGN.md`，Codex 两轮评审）。M1.0 实现计划：`plans/2026-08-29-m1-0-scaffold.md`。

---

## P0 — 搭竖切之前必须落地

### FIX-1 · 内核启动韧性（对应 F1）

**变更点**
- `cmd/kernel/main.go`：`manifest.Load` 失败、`reg.AddManifest` 失败 → 不再 `log.Fatalf`；
  记录该插件为 `FAILED`（新增 `sup.MarkFailed(path, reason)` 或复用 `MarkBlocked` 语义），
  继续加载其余插件。
- 仅"内核自身无法启动"的错误保持 fatal：contract catalog 加载、fence root、policy 加载、
  gateway bind。
- `gateway` 的 bind 提前到插件启动循环**之前**（或并行），使 client socket 不被慢/挂插件
  阻塞（同时缓解集成套件 3s 竞速）。

**验收**
- 新增 `cmd/kernel` 或 `internal/supervisor` 级测试：放入 1 个坏 manifest（非法 JSON）
  + 2 个正常插件 → 内核正常起，坏插件状态 `FAILED`，正常插件 `READY`，client socket 可用。
- **致残对照**：把"跳过坏 manifest 继续"的逻辑改回 `return err` → 该测试必须变红。
- 回归：`m05_qualification.py` 仍 PASSED。

**影响面**：`cmd/kernel/main.go`（~20 行）+ `internal/supervisor`（新增 1 个状态设置方法）。
不碰 router / registry / authz。

---

### FIX-2 · 注册表回收死亡 runtime（对应 F2）

**变更点**
- `internal/registry/registry.go`：新增 `func (r *Registry) RemoveRuntime(runtimeID string)`
  —— 从每个 `r.providers[key]` 切片移除该 runtime 的 `Provider` 条目；若它是某 authority 的
  active writer，走既有 `clearWriterIfUnhealthyLocked` 语义（保留 epoch）。
- `internal/supervisor/supervisor.go:watch`：`s.reg.MarkHealth(run.ID,false)` 之后、
  `router.RemoveClient` 之后，调用 `s.reg.RemoveRuntime(run.ID)`。
- 保序：先 `MarkHealth(false)` 触发依赖 reconcile（让依赖方感知 unhealthy），再 `RemoveRuntime`。

**验收**
- `internal/registry/registry_test.go`：注册 2 个 runtime → `RemoveRuntime` 其一 →
  `Providers(name,major)` 长度 -1；`ResolveForKind` 不再返回被移除者；若被移除者是 writer，
  `writers[ak].RuntimeID == ""` 且 `Epoch` 不变。
- `internal/supervisor` 级：插件 crash → restart 5 次 → `len(reg.Providers(...))` 稳定，
  不随重启次数增长。
- **致残对照**：注释掉 `watch` 里的 `RemoveRuntime` 调用 → supervisor 级测试必须变红。

**影响面**：`registry.go`（~25 行新方法）+ `supervisor.go`（1 行调用）。
`ResolveForKind` / `MissingHealthy*` 逻辑不变（它们已在遍历时跳 unhealthy，此修复是清理而非改语义）。

---

### FIX-3 · 写超时 + 事件扇出隔离 + stream 段隔离（对应 F3 / F4 / F5）

三项同源（"一个坏插件不得拖垮全局"），一起做、分 commit。

**F3 — ProcessClient.Send 写超时**
- `internal/router/client.go`：`Send` 增加带超时的写。方案：写操作放独立 goroutine +
  `select { case <-done: case <-time.After(writeTimeout): }`，超时返回错误并将该 client 标记
  为坏（触发 `RecordFailure` / 后续 `RemoveClient`）。`writeTimeout` 默认 5s，可配。
- 调用方 `pc.Call`、`publishEnvelope`、`RemoveClient` 的 `Send` 错误路径确认都已处理（Call
  已 return err；publishEnvelope 见 F4；RemoveClient 忽略即可）。

**F4 — 事件扇出 log-and-continue**
- `internal/router/router.go:publishEnvelope`：订阅者 `c.Send(e)` 失败 → 记录（结构化日志）
  并 `continue`，不 `return err`。整个扇出循环结束后返回 nil（或聚合错误但不中断）。

**F5 — stream 段隔离**
- `internal/router/router.go:forwardStream`：`sr.external <- e` 改为非阻塞 +
  有界等待；超时/满 → 合成 `KindStreamClose{Code: "BACKPRESSURE_LIMIT"}` 发给消费者，
  向 provider 发 `KindCancel`，`delete(r.streams, streamID)`。复用 `client.go:67-86` 已有
  的模式，抽成公共 helper。

**验收**
- F3：新增 router 测试 —— 一个"从不读 stdin"的假插件（`io.Writer` 阻塞）→ 对它的
  `pc.Call` 在 `writeTimeout` 内返回错误，不永久挂；其它插件的调用不受影响。
- F4：`publishEnvelope` 测试 —— 3 个订阅者，第 1 个 `Send` 报错 → 第 2、3 个仍收到事件。
- F5：`forwardStream` 测试 —— 外部 channel 不消费 → provider 的**其它** stream 仍能推进；
  卡住的 stream 收到 `BACKPRESSURE_LIMIT` close。
- **致残对照**：
  - F3：写超时逻辑置为 `<-done`（去掉 timeout 分支）→ F3 测试挂起/超时失败。
  - F4：改回 `return err` → F4 测试变红。
  - F5：改回阻塞 `sr.external <- e` → F5 测试挂起。
- 回归：`go test -race ./...` + `m05_qualification.py` 全绿（尤其 stream SIGKILL / cancel / disconnect 用例）。

**影响面**：`internal/router/client.go`（~30 行）+ `internal/router/router.go`（~25 行）。
是全审查里对并发路径改动最大的一项，`-race` 必须干净。

---

## P1 — 竖切期间并行推进

### FIX-4 · composition fitness function（对应 A1）—— 与竖切设计绑定

**变更点**
- `architecture-tests/check_boundaries.py`（或新增 `check_composition.py`）新增规则：
  1. 单插件 `consumes.required` + `consumes.optional` 总数 > 阈值（初值建议 6）→ 警告；
     > 更高阈值（初值 10）→ 失败。可在 manifest 加 `"composition": true` 声明豁免上限，
     但此时强制规则 2。
  2. 声明 `composition: true` 的插件**不得**有 `mode: stateful` 的 export（编排不持久化业务态）。
  3. 单插件实现源码行数 > 阈值（初值 800，注释/空行除外）→ 警告 —— C36 本地可理解性。
  4. （可选）契约扇出图：打印每个 composition 插件的能力依赖树，供人工评审。
- 阈值写进 `architecture-tests/thresholds.json`，改阈值需在 commit message 说明理由。

**验收**
- 用现有 `workflow-demo`（consumes 5 个，非 composition 声明）跑 → 通过。
- 构造一个 consumes 12 个能力的假 manifest → 失败。
- 构造一个 `composition:true` 且 export stateful 的假 manifest → 失败。
- **致残对照**：把三条规则的 `errors.append` 全注释 → 上述反例测试必须由红转绿（证明规则在起作用）。
- 接一个真实下游消费者：CI 里 `check_boundaries.py` 与 `check_composition.py` 都进 `scripts/test.sh`。

**影响面**：纯 CI/护栏脚本，不碰 Go 源。**这是防方向再偏的核心交付。**

---

### FIX-5 · 契约与内核分家（对应 A3）

**变更点**
- 新建仓根 `contracts/`（与 `kernel/` 平级）。把 `kernel/contracts/` 里的域契约
  （`work.*`、`event.journal.*`、`agent.execute`、`workflow.demo.run`）迁出。
- `kernel/contracts/` 只保留 `*.probe` 对抗 fixture（内核自测用）。
- 调整 `kernel/cmd/kernel/main.go` 的 `-contracts` 默认路径 / 或启动时合并多个 catalog 目录。
- `kernel/policy.json` 拆：内核自测 policy 与项目 policy 分开。
- `tools/generate_contracts.py`、`architecture-tests/check_boundaries.py` 的 ROOT 假设跟着调整。

**验收**
- `go test ./...` + `scripts/test.sh` 全绿（内核用 probe 契约自测）。
- 项目级：竖切插件用仓根 `contracts/` 的域契约，`check_boundaries.py` 对合并 catalog 通过。
- **致残对照**：删掉迁出的某个域契约 schema → 对应竖切插件 admission 失败。

**影响面**：目录迁移 + 3 个 python 工具的路径假设 + 内核启动参数。**建议在竖切设计定稿后、
写竖切代码之前做**，避免路径来回改。

---

## P2 — 硬化，不阻塞主路径

| 项 | 对应 | 动作 | 验收 |
|---|---|---|---|
| FIX-6 | S1 | `persistFenceLocked` 移出 `r.mu` 写锁：`ResolveForKind` 内先在锁下确定"要提升为 writer"并占位，释放锁后做 flock+落盘，再回锁提交 `writers[ak]`。或改为异步持久化 + 启动恢复兜底。 | 并发压测：N 个 goroutine 对同一 authority 发 command，registry 读操作 p99 延迟不受磁盘 I/O 影响；fencing 语义（stale writer 拒绝）不变，`m05` fence-probe 用例仍 PASS。致残：回退到锁内落盘 → 压测延迟指标回归。 |
| FIX-7 | S8 | `persistFenceLocked` 补 tmp `f.Sync()` + 目录 `Sync()`，与 `work-registry.save()` 对齐。 | 崩溃注入测试（kill -9 在 rename 前后）→ 重启后 epoch 单调不倒退。 |
| FIX-8 | S2 | gateway `serve` 入口集中调用 `sanitizeInbound(&req)`：清 `Principal/ActorChain/DelegationID/FencingEpoch/Contract/Caller`，只保留客户端可控的 `Capability/Major/Kind/Service/Authority/ProviderHint/Payload/IdempotencyKey/Deadline/StreamID`。 | 恶意 wire 测试：客户端塞 `Principal:"root"` + `FencingEpoch:999` → provider 收到的 envelope 里这些字段为内核重写值。致残：注释 `sanitizeInbound` → 测试变红。 |
| FIX-9 | S3 | `work.transition@1` 契约：`expected_version` 改为**必填**（新 minor，`compatibility: backward-within-major` 需评估）或新增 `idempotency_key` 支持并在 handler 去重（复用 `work.create` 的 `Dedupe` map 模式）。 | 重试测试：同一 transition 请求发 2 次（带 idem key 或带过期 expected_version）→ version 只 +1。致残：去掉去重 → 测试变红。 |
| FIX-10 | S4 | `ProcessClient.readLoop` / `pluginhost.Serve` 对解析失败的行写 stderr 一条 `WARN plugin=<id> unparseable line`（限流，避免刷屏）。 | 手动：插件打一行 `garbage` → 内核 stderr 有 WARN，插件不失效。 |
| FIX-11 | S5 | 删除 `event-journal` 死代码 `func (j *journal) append()`；`verifyAndLoadTail` 保留。 | `go vet` / `go build` 通过；journal 集成用例不变。 |
| FIX-12 | S6 | 决策：`go.mod` 降到实际所需（测试确认 1.19 可编译，建议定 `go 1.21` 留 `toolchain` 空间）+ CI matrix 固定一个 Go 版本。 | CI 在 pinned 版本绿；`go build` 在 1.21 通过。 |
| FIX-13 | S10/S11 | 清死代码赋值 + 修错位注释。 | `go vet` 干净。 |
| FIX-14 | S9 | `residentBytes` macOS 路径：缓存 `ps` 结果 / 批量一次 `ps` 查所有插件 pid / 拉长采样周期到 2s。 | 采样 CPU 开销下降；内存超限仍能在 ≤2s 内触发 kill（`m05` memory budget 用例 PASS）。 |
| FIX-15 | S7 | 不改 `work-registry`（参考实现），但在 `docs/PLUGIN-STORAGE-GUIDANCE.md` 明确："真实 Foundation 插件用 SQLite/WAL + 增量写，禁止全量 JSON blob"。竖切的 state 类插件遵循。 | 文档 + FIX-4 的 review 清单覆盖。 |

---

## 交付顺序

```
P0: FIX-1 → FIX-2 → FIX-3           （内核 v0.11.0，独立 review + 致残对照）
        ↓
竖切设计定稿（A2 决策 + 插件边界图）
        ↓
FIX-5（契约分家）→ FIX-4（fitness function，随首个 composition 插件落地）
        ↓
竖切实现（真实 Engineering Workflow）
        ↓
P2: 硬化项按需穿插，不阻塞
```

## 不做 / 登记为观察

- 集成套件 `wait_socket` 3s 竞速：FIX-1 把 gateway bind 提前后自然缓解；若仍偶发，
  把 100 次改 300 次即可，不单独立项。
- 内核词汇黑名单可绕过：已知局限，AST 级检查成本高、收益低，维持现状 + 人工评审。
- `digest()` 依赖 `Payload` 原始字节序 → 跨语言 journal replay 会分歧：竖切阶段只有 Go
  journal，不阻塞；若将来有第二实现，届时定 canonical JSON 规范。
