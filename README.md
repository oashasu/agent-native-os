# Agent-Native OS

Agent-Native Engineering Work OS 的 Go 实现仓库：**领域无关微内核**（`kernel/`）+ **工程竖切 M1**（`plugins/`、`contracts/`、`cli/`）。

## 从哪里开始

| 文档 | 回答什么 |
|---|---|
| [`docs/VISION-AND-ROADMAP.md`](docs/VISION-AND-ROADMAP.md) | **目标基线**：项目远景、中期目标、长期目标、里程碑编号对齐、历史素材出处 |
| [`docs/HUMAN-CONSOLE-UI-DESIGN.md`](docs/HUMAN-CONSOLE-UI-DESIGN.md) | **Human Console UI 设计**：轻量 IDE + Agent 会话工作台（两镜头 + 切换器，ADR-002 的落地设计；载体 = VSCode 主仓库二开） |
| [`docs/ADR-005-near-term-lightweight-ide-and-session-manager.md`](docs/ADR-005-near-term-lightweight-ide-and-session-manager.md) | **近期主线转向**：轻量图形化 IDE + Agent 会话管理（对应 IDEA 内存重、会话难管两个真实痛点；M1.9 暂缓） |
| [`vscode-agentdeck/README.md`](vscode-agentdeck/README.md) | **VSCode 扩展原型**：会话树、tmux 托管终端标签、会话改动 diff、Agent↔IDE 视图切换（复用 agentdeck 服务；11 项真实 VS Code 集成测试） |
| [`agentdeck/README.md`](agentdeck/README.md) | **会话管理器（已可用）**：claude/codex 历史会话索引、tmux 托管防误关、休眠/恢复、状态标签筛选 |
| [`docs/ADR-004-vscode-fork-ui-carrier.md`](docs/ADR-004-vscode-fork-ui-carrier.md) | **UI 载体决策**：基于 microsoft/vscode 官方主仓库二次开发（重开/取代 ADR-003+012 的 TUI 首选） |
| [`prototypes/human-console.html`](prototypes/human-console.html) | **UI 可点击原型 v3**：浏览器直接打开，体验层级目录树 / 两镜头切换 / ⌃K 换根 / ⌃S 会话导航 / ⌃W 工作空间 / **Cursor 式 Agent 工作台**（改动树 revert / hunk 级 diff 审阅 / checkpoint / 工具审批 / composer）/ 完成闸 |
| [`docs/M1-DESIGN.md`](docs/M1-DESIGN.md) | **实现基线**：M1 工程竖切的命题、6 条 Gate、里程碑拆分（M1.0–M1.9）与验收场景 |
| [`kernel/README.md`](kernel/README.md) | 微内核 v0.10.0（M0.5 对抗资格认证）的架构、边界与限制 |
| [`docs/superpowers/`](docs/superpowers/) | 各里程碑的 spec / 实现计划 / 派工提示 / 交接文档 |

## 快速验证

```bash
bash kernel/scripts/test.sh    # 微内核全量回归（含 M0.5 对抗认证）
bash scripts/smoke.sh          # M1 live-kernel 冒烟
bash scripts/check-arch.sh     # 契约 / composition / 词汇护栏检查
( cd agentdeck && GOWORK=off go test ./... )   # 会话管理器测试
( cd vscode-agentdeck && npm test )            # VSCode 扩展：单元 + 真实 VS Code 集成测试
```

## 状态速览

- **近期主线（2026-10-02 起，[ADR-005](docs/ADR-005-near-term-lightweight-ide-and-session-manager.md)）：轻量图形化 IDE + Agent 会话管理**——解决 IDEA 常驻 10G+ 内存、claude/codex 会话难管两个真实痛点。
  - ✅ `agentdeck/` 会话管理器已可用（分支 `feature/agentdeck`）。
  - ✅ `vscode-agentdeck/` VSCode 扩展原型（stock VSCode，无需 fork）：会话树 / 终端标签 / 改动 diff / 视图切换。
  - ⏭ 下一步：真实使用并收集反馈；会话托管契约先行下沉为 Go 插件；量化内存收益。
- 微内核 M0.5 对抗资格认证：**PASSED**（边界仍为候选，非生产安全认证）。
- M1 竖切：M1.0–M1.8.5 `— done`（tags 至 `m1.8.5-…`）；**M1.9 qualification 暂缓**（只有计划，未开始实现，见 `docs/superpowers/plans/2026-09-07-m1-9-qualification.md`；暂缓原因见 ADR-005）。
- UI-1（VSCode fork 改壳）：暂停；fork 工作副本已丢失，仓库内仅留规格与 S1 补丁。
