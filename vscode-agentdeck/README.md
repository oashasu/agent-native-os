# AgentDeck — VS Code 扩展（原型）

把 [`agentdeck`](../agentdeck/README.md)（claude / codex 会话管理器）放进 VS Code：会话树、tmux 托管的终端标签、会话改动文件 diff、Agent ↔ IDE 视图切换。扩展只做显示，会话逻辑全部在 agentdeck 服务里（`127.0.0.1:47017`），所以关窗口、关 VS Code 都不会中断运行中的会话。

## 功能

| 位置 | 作用 |
|---|---|
| 侧边栏「Agent 会话」 | 分组：运行中 / 置顶 / 已标记 / 最近 / 更早；单击**运行中**的 = 接入终端；单击**休眠**的 = 只读预览最近对话（不启动进程、不占内存），点 ▶ 恢复。右键：状态、标签、置顶、改名、把目录加入工作区 |
| 编辑区终端标签 | `tmux attach` 到所选会话。**关闭标签只是断开，agent 继续跑** |
| 侧边栏「会话改动」 | 所选会话目录的 `git status`；点文件打开 VS Code 原生 diff（HEAD ↔ 工作区） |
| 状态栏 | `N 运行 · xxMB`，点击快速切换 |
| `⌘⌥K` | 快速切换（搜索标题/目录/标签，休眠的自动恢复） |
| `⌘⌥1` / `⌘⌥2` | Agent 视图 / IDE 视图：互相切换时各自记住焦点（上一个终端 / 上一个代码文件），不关闭任何东西 |

## 运行（不需要安装 VS Code 到系统）

```bash
cd vscode-agentdeck
npm install
npm run vscode          # 首次会下载一份便携 VS Code 到 .vscode-test/，并带着本扩展启动
```

已装 VS Code 的话：`code --extensionDevelopmentPath=$(pwd)`，或在 VS Code 里按 F5。

扩展会自动查找并启动 agentdeck 服务（找 `agentdeck.binaryPath` → 仓库 `.bin/agentdeck` → `~/agent-native-os/.bin/agentdeck` → PATH）。先编译：`cd agentdeck && GOWORK=off go build -o ../.bin/agentdeck .`

## 配置

`agentdeck.port`（默认 47017）、`agentdeck.binaryPath`、`agentdeck.autoStart`、`agentdeck.tmuxSocket`（默认 `agentdeck`）、`agentdeck.refreshSeconds`。终端字体用 VS Code 自带的 `terminal.integrated.fontSize` / `⌘+`。

## 测试

```bash
npm run test:unit          # 纯逻辑（分组、git status 解析…），node:test
npm run test:integration   # 在真实 VS Code 里端到端：隔离 HOME / tmux socket / 端口 + 假 claude，不碰真实会话
```

## 已知限制（原型）

- 扩展无法改标题栏和整体布局；「视图切换」是同一窗口内的焦点/侧边栏切换，不是全屏互斥布局。需要壳层改动才考虑 fork（ADR-005）。
- 没有 VS Code 终端输出读取 API，终端内容的断言用 tmux `capture-pane` 完成。
- `agentdeck` 服务由扩展以 detached 方式启动，VS Code 退出后继续运行（tmux 里的会话本来也继续运行）。
