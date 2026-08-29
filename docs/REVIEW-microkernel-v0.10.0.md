# 微内核代码审查 — agent-native-microkernel v0.10.0

审查日期：2026-08-29
审查范围：`kernel/`（源自 `agent-native-microkernel-v0.10.0-adversarial-qualified`）
审查方式：通读 `internal/` 全部 Go 源（~5k 行）、`sdk/go/`、`plugins/`、13 个契约 schema、13 份 `docs/`；本机复跑验证。

## 0. 复跑验证结果（不采信自报）

| 项 | 命令 | 结果 |
|---|---|---|
| 单元测试 | `go test ./...` | PASS |
| 竞态检测 | `go test -race ./...` | PASS |
| 生成契约 SDK 新鲜度 | `python3 tools/generate_contracts.py --check` | UP TO DATE (13) |
| 契约兼容性 | `python3 tools/test_contract_compat.py` | PASSED |
| 契约 schema/catalog | `python3 tools/contract_check.py` | PASSED (13, Draft 2020-12) |
| 架构护栏 | `python3 architecture-tests/check_boundaries.py` | PASSED (13 manifest / 13 契约) |
| M0.5 对抗集成套件 | `python3 tests/integration/m05_qualification.py` | **PASSED** |

注：`bash scripts/test.sh` 一次性执行时集成套件会失败，根因是 `wait_socket` 只等 3 秒
（100×30ms），冷构建时内核要起完 13 个插件才 bind client socket，竞速失败。属测试脚手架
脆弱，非内核缺陷（但与 F1 同源：内核 client socket 在所有插件启动完成后才开放）。

## 1. 总判断：方向没有偏

内核严格停在 L0：manifest / supervisor / registry / router / authz / health + 契约平面
+ 单写者 fencing。Constitution 40 条 + kernel membership test 自洽；`check_boundaries.py`
的内核词汇黑名单（虽可绕过）确实拦住了 Task/Session/Workflow 进内核。M0.5 对抗矩阵覆盖
的是真实威胁（confused deputy、query 冒充 command、provider error 被吞、stale writer、
journal 篡改、in-flight SIGKILL 等），且本机复跑除脚手架竞速外全绿。

**但要认清两点：**

1. **这是一台对抗测试台，不是平台。** 13 个契约里约 8 个是 `*.probe` 对抗 fixture。真实域
   能力只有 `work.*`、`event.journal.*`、`agent.execute`（mock）、`workflow.demo.run`。
   没有 state/blob/artifact、scheduler、notification、secret、真实 harness adapter、
   archive/recovery、带 gate 的 workflow engine —— 这些 AEOS M1–M11 在 Rust 单体里全做了。
   缺口不在内核，在内核之上一片空白。
2. **防"再次单体化"的可执行护栏没建**（见 A1）。给定上一轮"功能都实现了但没解耦"的历史，
   这是最该补的东西，而它现在还是缺的。

## 2. 架构层面的缺口

### A1 — 缺少 composition 监控的 fitness function（最重要）

`architecture-tests/check_boundaries.py` 检查了跨插件 import、契约一致性、内核词汇纯度，
但**完全没有**检查：

- 插件扇入（`consumes.required` 数量）；
- 编排集中度 / 单插件源码规模；
- composition 插件是否持有私有 stateful authority。

C36（本地可理解性）/ C37（编排归 composition，原子插件保持专注）纯靠人肉评审。上一轮的
方向偏移正是这个机制缺失造成的。真实工作流一旦落地，"一个 workflow-demo 式插件把 work +
journal + agent + 20 个能力全内联"就是 AEOS 单体在插件层复现。

### A2 — 请求作用域的 delegation 在 `invoke` 返回即销毁

`internal/router/router.go:304` 的 `defer delete(r.delegations, delegationID)`：委派上下文
在 `invoke` 返回（即 provider 发回 Result/Error）后立即删除。任何 composition 插件在返回
响应**之后**做异步工作（后台 step、延迟副作用），都会丢掉委派授权 —— 对抗套件正是靠这个
证明"延迟副作用不会继续"（`cancel.probe`）。

**影响竖切设计：** 真实 workflow 插件的编排必须二选一 ——
(a) 在单次 composition 调用内**同步**完成所有 step；或
(b) 该插件从 host policy 拿 `service_authority`，自己持有**可对账的持久状态机**，不靠 delegation。

### A3 — 内核契约与内核代码没分家

`agent.execute@1`、`workflow.demo.run@1`、`work.*` 的 schema + policy grant 打在内核包
`kernel/contracts/` 与 `kernel/policy.json` 里，与 C26（契约独立拥有）有张力。整合时内核
仓应只留 probe fixture，域契约移到独立目录/包。

## 3. 代码缺陷

### 承重面（一个坏插件可拖垮全局 / 长期运行退化）

| # | 位置 | 问题 | 证据 |
|---|---|---|---|
| **F1** | `cmd/kernel/main.go:71,74` | 单个坏 manifest → `log.Fatalf` 整个内核挂。与 `docs/04-SECURITY-FAILURE-MODEL`「provider start failure is local, host continues」直接矛盾。manifest JSON 解析失败、或一个插件 admission 冲突 → 全内核起不来。 | 读源确认；`grep -n Fatal cmd/kernel/main.go` 命中 71/74/39/47/52/108 |
| **F2** | `internal/registry/registry.go` | **无 `RemoveRuntime`**。插件 crash/restart 后旧 `Provider` 条目永久留在 `r.providers[key]`（仅被 `MarkHealth` 标 unhealthy）。长期运行 + 插件抖动 → `r.providers` 无界增长，`ResolveForKind` / `MissingHealthyRequired` 线性扫描随之变慢。 | `grep "func (r *Registry)"` 无 remove/prune/unregister；`supervisor.watch` 只调 `MarkHealth(false)` + `delete(s.runs,...)`，不通知 registry 删 provider |
| **F3** | `internal/router/client.go:104` `ProcessClient.Send` | 用 `json.Encoder.Encode` 直写子进程 stdin，**无 deadline**。一个不读 stdin 的插件（卡死 / crash 中，64KB pipe 缓冲满）会阻塞：`pc.Call` 的发送阶段（`Call` 的计时器只覆盖响应等待，不覆盖 `c.Send`）、`publishEnvelope` 事件扇出、`RemoveClient` 发 stream close。 | 读源确认 `Call`（client.go:109-135）先 `c.Send` 后起 timer |
| **F4** | `internal/router/router.go:426-428` `publishEnvelope` | 事件扇出遇首个订阅者 `c.Send` 错误即 `return err`，其余订阅者收不到该事件。应 log-and-continue。 | 读源确认 |
| **F5** | `internal/router/router.go` `forwardStream` | `sr.external <- e`（buffered 64）会因慢 / 满的外部客户端阻塞 `serveStreamInbound` → **同一 provider runtime 的所有 stream 一起卡**。已实现的 backpressure 只覆盖 provider→ProcessClient 段（`client.go:67-86` 合成 close），ProcessClient→外部段没做。 | 读源确认；对照 `client.go` 的 `streamEvents` 有界处理 vs `forwardStream` 无 fallback |

### 次要 / 硬化

| # | 位置 | 问题 |
|---|---|---|
| S1 | `registry.go:110` `persistFenceLocked` | writer 提升在 `r.mu` 写锁内做 `syscall.Flock(LOCK_EX)` + 文件 write + rename（无 fsync tmp/dir，见 S8）。stateful command 触发提升时整个 registry（所有健康检查、心跳记账、其它路由解析）阻塞在磁盘 I/O 上，且与插件 `fencing.WithWriteFence` 争用**同名** lock 文件（`fenceSafe` 与 `fencing.safe` 规则一致）。 |
| S2 | `internal/clientgateway/gateway.go:73` `serve` | 入口只设 `req.Caller = wire.Identity`，不清 `req.Principal / ActorChain / DelegationID / FencingEpoch / Contract`。当前靠下游每条路径逐一覆写兜底（`router.invoke:274-285` 基本覆盖到了），但客户端辅助里那个 `prepare()`（gateway.go:137）服务端**根本没调用**。TCB 边界应主动清洗而非依赖"每条路径都记得覆写"。 |
| S3 | `plugins/work-registry/main.go:145` `work.transition` | 无幂等键；契约 `work.transition@1` 里 `expected_version` 可选。handler 标了 `Retryable`（FENCING_OR_IO），但不带 `expected_version` 的重试会二次推进 `version`（违反 C11）。 |
| S4 | `internal/router/client.go:52` / `sdk/go/pluginhost/host.go:280` | `json.Unmarshal(line) != nil` → 静默 `continue`，无任何日志。插件误打一行 debug stdout 就神秘失效，排障困难。 |
| S5 | `plugins/event-journal/main.go:84-112` | 死代码 `func (j *journal) append()` **不带 fence**，与 handler 内联的 fenced 版本（130-157）并存 —— 复制粘贴地雷。 |
| S6 | `go.mod` | 声明 `go 1.23`；本机 `go1.19.1` 能编能测**全过** → 代码并不需要 1.23。要么放宽指令，要么 CI 钉死 1.23，否则换机器（Go ≥1.21 且无 1.23 工具链）构建会因 toolchain 指令炸。 |
| S7 | `plugins/work-registry/main.go:37,53` | 每次 create/get/transition 都 `refresh()` 全量读盘 + `save()` 全量 `MarshalIndent` 重写 + 文件 fsync + 目录 fsync。O(n)/op 的 JSON blob store。参考实现可接受，但**此模式不得抄进真实 Foundation 插件**。 |
| S8 | `registry.go:134-138` `persistFenceLocked` | 写 lease 用 `WriteFile(tmp)` + `Rename`，但 **tmp 未 `Sync()`、目录未 `Sync()`**。对比 `plugins/work-registry` 的 `save()` 做了完整 fsync 链。fencing epoch 是崩溃恢复的关键持久状态，这里的持久化强度弱于它保护的数据。 |
| S9 | `internal/supervisor/supervisor.go:326` `residentBytes` | macOS 路径每 750ms/插件 fork 一次 `ps`。13 插件 ≈ 17 fork/s 常态开销；"数百插件"场景会明显。 |
| S10 | `internal/manifest/manifest.go:108` | `for _, c := range m.Exports { ... c.Mode = "stateless" }` —— `c` 是副本，赋值是死代码（后续 `c.Mode=="stateful"` 判断读的是原始非空值，行为恰好正确，但是坏味道）。 |
| S11 | `internal/registry/registry.go:391` 注释悬空 | `// RecordFailure is the request-path failure score/circuit breaker...` 注释后紧跟的是 `MarkDependencyHealth`，注释与函数错位。 |

## 4. 已验证为「非缺陷 / 有意设计」

- **confused-deputy 防护链完整**：外部 identity → workflow-demo（fresh delegation 绑定其 runtime）→ `work.create`（principal 保持为外部 identity，`ScopeAllows(delegationScope,...)` 放行）。`workflow-user` 直接 `work.create` 被拒、经委派成功 —— 读源逐跳验证一致。
- **provider `KindError` 作为成功路由响应返回**（`router.go:328-332`），`RemoteError` 保留 code/retryable/details —— 正确。
- **`work.get` 声明 `mode: stateful` + `kind: query`**：`ResolveForKind` 对 stateful 非 command 返回权威内任一健康副本 —— 符合"读可用任一副本"设计。
- **心跳成功不清 request-circuit**（`registry.go:458` 显式注释）—— 正确，进程能应答 ping 不代表业务 handler 没挂。
- **journal fsync + SHA-256 前向哈希链 + replay 校验**（`plugins/event-journal/main.go`）—— tamper-evident 实现正确；跨语言 replay 需注意 `digest()` 依赖 `Payload` 原始字节序（见多语言注意）。

## 5. 整合 / 竖切前的建议（详见 FIX-PLAN）

1. **先补 A1 的 fitness function** —— 防你重蹈覆辙的唯一可执行手段。
2. **修 F1、F2** —— 便宜且承重，搭东西之前修掉。
3. **F3/F4/F5 一起修**（写超时 + 事件扇出隔离 + stream 段隔离）—— 同一类"一个坏插件拖垮全局"。
4. **定 A2** —— workflow engine = 薄 composition 插件（同步 step + gate 求值做纯读模型），
   或 `service_authority` + 自持久状态机；每个"工具"（build/test/git/review/PR）= 各自
   原子插件 + 各自契约 + 各自 authority。AEOS 的 schema 按插件拆，**绝不**做共享 `store` crate。
