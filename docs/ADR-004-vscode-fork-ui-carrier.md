# ADR-004 — UI 载体定向：基于 VSCode 官方主仓库（microsoft/vscode）二次开发

日期：2026-09-19（会话内定向）
状态：**已定向**（项目所有者确认，推翻 ADR-003 §决定 3 的低后悔默认）
关联：
- **取代** ADR-003 §决定 3 与 ADR-012（"首期 TUI/Neovim"生效项）
- 沿用 ADR-002（一个 WorkContext、两个镜头、⌃K 换根、UI 不拥有状态 + 读投影契约）——**交互模型本身不随载体动摇**
- 关联 ADR-001（Go）、`M1-DESIGN.md`（契约面 / DONE 不变量 / 里程碑链——零影响）
- 原型规格：`prototypes/human-console.html`（载体无关的可点击规格）、`docs/HUMAN-CONSOLE-UI-DESIGN.md`

---

## 背景：ADR-003 的触发信号已成立

ADR-003 记录了 UI 阶段的决策标准，并列了三个"把决定拿回来重议"的信号。对照现状：

1. **"出现一个成熟可嵌入的 GUI 编辑器组件，把'造编辑器'的成本降到可接受" —— 触发。**
   不是"可嵌入组件"，而是**成熟的编辑器产品壳**：VSCode 官方主仓库（MIT）本身就是可 fork 的编辑器全栈
   （编辑器 / 树 / 多 tab / diff / LSP / 调试 / 终端 / 主题 / 扩展系统）。业界已验证：Cursor、Windsurf、
   VSCodium 均走此路线。ADR-003 表里 TUI 的"替代 IDEA 天花板"（`jdtls` 封顶、无图形调试器、Spring 感知弱）
   在 VSCode 壳下全部不成问题；"造编辑器"条目（Tauri/GUI 选项的最大成本）被 fork 直接清零。
2. **目标用户不是 Neovim 用户 —— 触发。** 项目所有者在原型评审中反复以 IDEA Project 窗口、DeepSeek Harness
   工作台为参照物；"直观"标准是 GUI 语汇。TUI 的渲染上限（树不能平铺、做不出 harness 式会话界面）成为实际阻碍。
3. "Java 工具链深度成为高频阻塞" —— 未验证，但 VSCode 方案天然带 Java 扩展生态（JDTLS / Spring Boot Tools /
   Test Runner），此风险同步消解。

## 决定

1. **首期 UI 载体 = 基于 `https://github.com/microsoft/vscode` 官方主仓库二次开发**（Code-OSS fork），
   替换 ADR-003 §决定 3 与 ADR-012 的 "首期 TUI/Neovim" 生效项。产品壳自主控制，品牌/遥测/商店条款按 MIT fork
   惯例处理（参照 VSCodium / Cursor）。
2. **交互模型与架构面原样继承 ADR-002 / ADR-004-之前的所有决定**：脊柱（WorkContext）唯一状态源、两镜头全屏
   互斥、⌃K 换根、UI 不拥有状态、7 个读投影数据契约。VSCode 二开只换"显示层 + 输入映射层"。
3. **数据/内核不动**：M1 Go 内核 + 投影契约不变；UI ↔ 内核经本地进程间通道（stdio JSON-RPC 或本地 HTTP）。
   **M1.9 Console 读投影充分性验收（M1-DESIGN §10）仍是本设计成立的前置机器判据**——与载体无关。
4. **键位映射保持"语义等同"**：⌃1/⌃2/F4 镜头、⌃K 上下文、⌃S 会话导航、⌃W 工作空间、j/k 树导航等语义
   原样保留；为从 Vim 迁移的用户提供 Neovim 键位兼容层（可选映射），不再反向绑定 TUI。

## 与 ADR-003 决策标准对照（重做该表）

| 维度 | Neovim / TUI（原默认） | **VSCode 主仓库二开（现定向）** |
|---|---|---|
| "替代 IDEA" 天花板 | `jdtls` 封顶：重构/图形调试器/Spring 感知差一截 | 原生树+多 tab+diff+LSP+图形调试器+扩展生态（JDTLS、Spring Boot Tools），上限高得多 |
| 构建 / 维护成本 | 一份 Neovim 配置 + 胶水层；但两镜头壳/会话界面要自造，效果上限低 | fork 维护要跟上游（团队级成本，Cursor/VSCodium 同路径）；首次构建重、占磁盘大 |
| 目标用户 | 已经住在 Neovim 里的人 | 从 IntelliJ 迁出来的人（本项目所有者类型）——软着陆 |
| 到可用 v1 的时间 | 快（配置+胶水），但 v1 = 打折的交互 | 中（先改壳验证两镜头，再落 Agent 工作台）；交互完整度从一开始就高 |
| 体积 / 内存（量级参考） | ~15 MB / ~40 MB | Electron：安装包 ~100 MB+ / 运行时 ~300–600 MB（相对 IDEA 的 ~2 GB / 分钟级启动仍是轻量级） |
| AI/Agent 会话界面 | TUI 渲染上限，降级 | TreeView + webview 原生玩法 = harness 式工作区/会话/对话流水直接做 |

补充取舍：Neovim 在**极致轻量、终端内/SSH、启动毫秒级**这三项上仍占优；若未来出现"终端伴侣版"需求，
可作为同一内核/契约下的第二个壳（可选，不阻塞本决定）。

## 落地顺序（对应 HUMAN-CONSOLE-UI-DESIGN §10，重排）

1. **UI-1 改壳验证**：clone Code-OSS → 替换默认 shell 为"脊柱 + 两镜头布局"（⌃1/⌃2/F4/⌃K 语义搬入）——
   这一层决定"两镜头全屏互斥"成立与否，最先验证。HTML 原型即交互规格。
2. **UI-2 Agent 会话工作台 + 接线**：内置 view container（工作区/会话导航树）+ webview（对话流水/工具卡/
   diff 审阅）+ 本地通道接 7 个读投影。
3. **UI-3 大规模**：M2 读模型接入 ⌃K/⌃S/⌃W 全量枚举、FTS/语义检索、Archive/Hibernate 操作面（G9/UX-SES-007）。
4. **UI-4（可选）**：终端 Neovim 伴侣版（同契约第二壳）——仅当出现 SSH/极轻场景需求。

## 对 M1 的影响

**无。** `M1-DESIGN.md` 的契约面、DONE 不变量、里程碑链、M1.9 读投影充分性验收全部不动；
UI 前的里程碑（M1.0–M1.9）不依赖载体。本 ADR 只把 ADR-003 悬着的方向落定为"VSCode 二开"，
并把 ADR-012（首期 TUI）标记为被取代。

## 证伪（这条决定的退出条件，持续监控）

1. fork 后"两镜头"壳在真实工程实践中被证明不如并排布局 → 回 ADR-002 证伪①（可改为可分屏）。
2. 跟上游的维护成本吞掉迭代速度，产品收益不补差 → 退化为"基于 stock VSCode 的扩展包"（放弃壳层自控）。
3. 用户实际环境（低内存终端 / SSH-only）主导 → 启用 UI-4 终端伴侣版作为事实主壳，VSCode fork 降级为桌面选项。
4. M1.9 Console 读投影充分性验收失败 → 与载体无关，两镜头设计本身不成立（ADR-002 同判据）。