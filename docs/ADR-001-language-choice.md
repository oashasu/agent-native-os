# ADR-001 — 实现语言选择：全 Go

日期：2026-08-29
状态：**已决（Accepted）**
决策人：项目所有者
背景输入：`REVIEW-microkernel-v0.10.0.md`

## 决策

内核、插件、SDK、工具链、CI 全部使用 **Go**。

- 内核保持 v0.10.0 的 Go 实现，不重写。
- 所有能力插件 / composition 插件 / adapter 用 Go，复用 `kernel/sdk/go/pluginhost`。
- AEOS（`agent-native-engineering-os` M1–M11，Rust）作为 **设计 / schema / SQL 参照**，
  归档于 `reference/aeos/`，只读。领域逻辑（workflow 阶段、gate 谓词、canonical event
  schema、archive 布局）**移植**为小 Go 插件，各自私有 schema，不整块搬 crate。

## 理由

| 维度 | Go | Rust |
|---|---|---|
| 内核现状 | 已完成、M0.5 对抗合格、~5k 行 | 需重写 → 丢失迭代最久的资产 |
| 系统形态 | 进程隔离 + newline-JSON over stdio + goroutine-per-request，Go 主场 | 需自写 async plugin host SDK，churn 风险 |
| 语言数 | 1（内核+插件+SDK+工具） | 2（混合仓，两工具链两 CI 两 SDK 永久协议同步） |
| 打包 | `go build` 出静态单文件二进制，交叉编译 trivial | 可行但更重 |
| 插件迭代速度 | 快；"很多小插件"场景反复受益 | 带 sqlx/tokio 冷编译几十秒起 |
| 领域建模 | 弱（const+string，无 sum type / 穷尽性）；可用表驱动纯读模型压制 | 强（非法状态无法表达）——在 gate/evidence 处有真价值 |
| AEOS 复用 | 移植算法与 SQL | crate 结构本就要拆（违反 C04/C09），"10.6k 行复用"部分是幻觉 |

**关键权重（对着偏移历史）：** 上一轮方向偏移的根因是 **集中**，AEOS Rust 单体正是那个
集中产物。复用其 crate 结构会把偏移带回来。真正对抗失败模式的是 Go + 很多小插件 +
补上缺失的 composition fitness function（见 REVIEW A1）。上次失败的不是类型安全，是边界。

## 后果 / 缓解

- **领域建模弱**：workflow 状态机与 gate 用"表驱动 + 纯读模型"实现；gate 前置条件写成
  显式布尔谓词列表（参照 AEOS M11 的 10 条）。
- **门未来仍敞开**：wire 协议语言中立。若竖切后某个具体插件的正确性负担明确值得第二套
  工具链，可单独为它写 Rust plugin host SDK 接入，成本可控且隔离。
- **AEOS 移植成本**：一次性，且与"按插件拆边界"是同一工作量。

## 关联

- 下一步：竖切设计（A2 决策：workflow 插件同步编排 vs `service_authority`+自持久状态机；
  插件边界图）。
- FIX-4（composition fitness function）随首个 composition 插件落地。
