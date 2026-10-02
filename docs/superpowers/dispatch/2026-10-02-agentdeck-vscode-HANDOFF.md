# 交接：近期主线转向 + agentdeck 会话管理器 + VSCode 扩展原型

日期：2026-10-02
写作者：Claude 会话 `5bc10415-1f13-4994-aa44-e5f86703f09c`（转录：`/Users/ada/.claude/projects/-Users-ada-agent-native-os/5bc10415-1f13-4994-aa44-e5f86703f09c.jsonl`）
用途：**合并分叉的会话**。后续会话读完本文即可接手，不需要重读转录。

> **接手更新（同日，终端里的那份会话）**：本文写于 agentdeck 窗口里恢复出来的分叉副本。真相已查明——`5bc10415` 被 agentdeck 恢复成第二个进程，对话在 19:46:21 分叉（分支 A = 本文作者，分支 B = 终端会话）。下文第 3 节所说的“他人改动”**不是别人，是分支 B 的改动**，现已**验证**（见第 3 节末尾）。分叉的完整经过和防护措施见第 11 节。
>
> 说明：本文区分三类信息——**已验证**（我跑过命令/测试看到结果）、**未验证**（写了但没亲眼确认）、**他人改动**（不是我做的，我只观察到）。

---

## 1. 项目所有者的真实目标（一切工作的判据）

- IDEA 常驻 10G+ 内存，整机 16G；日常主力已转向 AI Agent 开发，IDE 只用于查/审代码、搜索定位、提交、看提交记录、编译、debug。
- claude / codex 会话开太多占内存；iTerm2 易误关，会话找回、切换不方便；Agent 与 IDE 之间切换有摩擦。
- 要**图形化**、IDEA 式交互的轻量 IDE（Neovim 路线试过，交互不满意），参考 Cursor 的视图切换。
- 保留 Go 内核 + 插件架构：初衷是把大工程拆成"内核 + 很多插件"，使每个插件能在 Agent 单次会话上下文里开发完。
- 复盘教训：此前 M1.0–M1.8.5 全是无界面验收，图形交互直到 9/19 才开始，需求被压到最后。**每个里程碑必须有用户可感知的验收。**
- 记忆文件：`/Users/ada/.claude/projects/-Users-ada-agent-native-os/memory/user-real-goal-lightweight-ide.md`

## 2. 本会话做了什么（时间线）

1. 分析项目现状：Go 内核 + M1 竖切；M1.0–M1.8.5 done；**M1.9 实际只有计划**（39 个复选框全空、`scripts/qualify-m1.sh` 不存在，README 原写"在途"是不准确的）；UI-1 的 VSCode fork 源码放在 `/tmp/vscode-ui1`，已被系统清空（已验证：目录 0B、无 `.git`）。
2. 与用户对齐后转向（ADR-005）：近期主线 = 轻量图形化 IDE + Agent 会话管理；M1.9 暂缓；IDE 载体先 stock VSCode + 扩展，必要时才 fork。
3. 做了 **agentdeck**（独立会话管理器，Go + 本地 Web 窗口）。
4. 按用户反馈迭代 agentdeck：修终端布局塌陷、列表事件委托、折叠/标记/标签/置顶/筛选/预览、claude 别名自动沿用、Unicode11+WebGL 渲染、字体可调。
5. 改 README / 路线图 / 新增 ADR-005。
6. 做了 **vscode-agentdeck**（VSCode 扩展原型）并在真实 VS Code 里做了 11 项端到端测试。

## 3. 仓库与分支状态（截至写文档时，已用命令核对）

- 仓库：`/Users/ada/agent-native-os`；远端 `https://github.com/oashasu/agent-native-os`（**公开仓库**）。
- 当前分支 `feature/agentdeck`，比 `main`（`a5687c8`）多 16 个提交，**已全部推送**，远端 = `109d8c0`。
- 本会话的 3 个提交：
  - `5c8f0f3` 新增 agentdeck
  - `edaf7c1` 文档：ADR-005 + README/路线图转向
  - `109d8c0` 新增 vscode-agentdeck + 文档状态更新
- 更早的 13 个提交是 UI-1（VSCode fork 壳）规格/补丁，原先只在本机，现已随此分支推上远端。分支 `feature/ui1-vscode-shell`（`d2503c2`）仍只在本机，但内容已包含在 `feature/agentdeck` 里。
- `main` 没有动过，没有开 PR。

### ⚠️ 工作区里有未提交改动（分支 B 所做，**已验证**，见本节末尾）

提交 `109d8c0` 之后，`agentdeck/` 下出现 5 个已修改文件（作者写文档时不知道来源）。**来源已查明：是同一会话 ID 的另一个进程（分支 B，终端里的那份）所做**：

| 文件 | 内容（来自 `git diff`） |
|---|---|
| `agentdeck/main.go` | 服务进程 `Setsid`（关终端不再 SIGHUP 杀服务）；服务日志写 `~/.config/agentdeck/server.log` |
| `agentdeck/tmux.go` | 活动时间由 `session_activity` 改为 `window_activity` |
| `agentdeck/server.go` | `/api/sessions` 响应头 `X-AD-Ver`（内嵌 UI 的 sha256），供已打开窗口检测升级 |
| `agentdeck/agentdeck_test.go` | 新增 `TestSessionsEndpointCarriesUIVersion` |
| `agentdeck/web/index.html` | 至少包含：断线横幅与自动重连、UI 升级自动刷新、标签页带 × 关闭、「工作中 / 有更新」指示与「◐ 有更新」筛选、重开时恢复上次打开的运行中标签、快捷键提示（⌃⇧W 关标签 / ⌃⇧S 休眠 / ⌃1-9 跳第 N 个标签）、标记弹窗快捷按钮、对"已过期会话"的一次自动重试（`openSession` / `attach`）。**我只看了 diff 的前半段，完整内容请以 `git diff` 为准。** |

**验证状态（分支 B 实测）**：
- Go 测试全绿；在 Chrome 里用真实数据验证过：会话在后台被结束后点标签自动恢复（修复“超时后无法恢复”）、`⌃1/⌃2` 跳标签、`⌃⇧W` 只关标签不杀会话、重开窗口恢复标签、服务掉线显示横幅且恢复后自动消失、工作中/有更新指示。
- 服务进程已脱离终端（`pgid==pid`，`tty ??`）。
- VSCode 扩展的 11 项集成测试用这版源码重新编译后全部通过（隔离环境）。
- 与字号快捷键无冲突：`⌘+ ⌘- ⌘0`（meta）与 `⌃1-9 / ⌃⇧W / ⌃⇧S`（ctrl）修饰键不同，且都已纳入 `isAppKey()`。

## 4. 架构与文件地图

```text
agentdeck (Go 服务, 127.0.0.1:47017)  ←──HTTP/WebSocket──  Web 窗口 (Chrome --app, xterm.js)
        │                                                  ←──HTTP────────  VSCode 扩展
        ├─ 索引 ~/.claude/projects/*/*.jsonl 与 ~/.codex/sessions/**/rollout-*.jsonl（过滤 codex 子代理线程）
        └─ agent 进程跑在独立 tmux server：tmux -L agentdeck（关窗口/杀服务都不中断会话）
```

### agentdeck（`/Users/ada/agent-native-os/agentdeck/`）

| 文件 | 职责 |
|---|---|
| `index.go` | 会话索引（claude: ai-title 优先、首条真实用户消息兜底；codex: 首行 session_meta + `session_index.jsonl` 的 thread_name），按 mtime 缓存 |
| `tmux.go` | tmux 托管：`Resume` / `New` / `Kill` / `ListLive`（含进程树内存）；`AGENTDECK_SOCKET` 可改 socket 名（测试隔离用） |
| `server.go` | HTTP/WS：`/api/sessions` `/api/resume` `/api/new` `/api/sleep` `/api/meta` `/api/preview` `/api/ide` `/ws/term`；鉴权（Host 校验 + token cookie + WS 同源 + POST 的 `X-AD: 1`） |
| `meta.go` | 状态/标签/置顶/别名存储（`~/.config/agentdeck/meta.json`）+ 无进程预览（读 jsonl 尾部 1MB） |
| `main.go` | 入口：`agentdeck`（启动+开窗口）/`serve`/`list`；**启动时读用户 shell 的 `claude` `codex` alias 作为默认参数**（`aliasArgs`，4 秒超时，含元字符的 alias 拒绝） |
| `web/index.html` + `web/*.js` `*.css` | 单页 UI；xterm.js 及 fit/unicode11/webgl 插件已离线内置 |
| `agentdeck_test.go` | Go 测试：索引、WS 桥接与"断开后会话存活"、鉴权、标记存储、预览解析、alias 解析 |

数据与进程（用户机器上）：
- 配置目录 `~/.config/agentdeck/`：`token`（0600）、`tmux.conf`、`chrome/`（独立 Chrome profile）、`server.log`（他人改动引入）、`meta.json`（用户首次标记后才有）、可选 `config.json`（`claude_args` / `codex_args` / `ide_cmd`）。
- 端口：用户真实实例 47017；我的开发测试用过 47018；扩展集成测试用 47031。
- 写文档时 47017 上有一个在跑的实例（`/Users/ada/agent-native-os/.bin/agentdeck -port 47017 serve`，启动于 20:37:58）。**已核对**：`.bin/agentdeck`（20:36:52）比他人改动的源码（`main.go`/`server.go` 20:33:55、`index.html` 20:35:13）新，且该实例的 `/api/sessions` 已返回 `X-AD-Ver` 头——即**他人改动已编译并正在这个实例上运行**，用户看到的窗口很可能已经是带那些改动的版本。重启命令：`pkill -f "agentdeck.*serve"; /Users/ada/agent-native-os/.bin/agentdeck`（不影响 tmux 里的会话）。

### vscode-agentdeck（`/Users/ada/agent-native-os/vscode-agentdeck/`）

| 文件 | 职责 |
|---|---|
| `package.json` | 视图、命令、菜单、快捷键（`⌘⌥K` 快速切换 / `⌘⌥1` Agent 视图 / `⌘⌥2` IDE 视图）、配置项；`npm run vscode` 启动带扩展的便携 VS Code |
| `src/extension.js` | 会话树、改动树、终端接入（`tmux attach` 开在编辑区标签）、状态栏、视图切换、虚拟文档（git 旧版本、会话预览） |
| `src/model.js` | 纯逻辑：分组、描述、`git status -z` 解析 |
| `src/api.js` | agentdeck 服务客户端 + 自动启动服务（找二进制：设置 → 仓库 `.bin/` → `~/agent-native-os/.bin/` → PATH） |
| `src/git.js` | `git status` / `git show` |
| `test/unit.test.js` | 7 项单元测试 |
| `test/runIntegration.js` `test/suite/index.js` | 11 项集成测试（真实 VS Code，隔离 HOME/tmux socket/端口 + 假 `claude`） |
| `test/vscodePath.js` `test/launch.js` | 便携 VS Code 下载与启动（兼容 1.140 起 `MacOS/Electron` → `MacOS/Code` 的改名） |

## 5. 验证现状

**已验证（我亲自跑过）**
- agentdeck：`go test` 通过；故意破坏 3 处（跨域放行、断开即杀会话、关闭子代理过滤）均变红；在 Chrome 里用真实数据（659 个会话）走过：恢复 claude/codex、打字到达 agent、双会话切换、终端布局、中文边框对齐（Unicode11+WebGL 前后对比）、列表渲染 9ms。
- vscode-agentdeck：单元 7/7（4 种破坏均变红）；集成 11/11（破坏休眠与接入后 4 项变红）。集成测试证明了核心承诺：**关终端标签不丢会话**、重开重新接入同一 agent、休眠后可再恢复。

**未验证 / 已知缺口**
- `npm run vscode`（`test/launch.js`）：只做了语法检查，没实际启动（会弹桌面窗口）。
- 滚轮：确认 claude/codex 都在备用屏幕且 tmux `mouse_any_flag=1`（滚轮由 agent 自己处理），但没有肉眼确认滚动效果。
- 字号快捷键（`⌘+ ⌘- ⌘0`）：只验证了脚本语法和测试，没在浏览器里看效果（当时浏览器调试工具已断开）。
- Chrome app 窗口额外占用的内存：**没有测量**；"轻量 IDE 内存显著低于 IDEA"这条核心收益也**还没有量化**。
- （已解决）第 3 节的未提交改动已由分支 B 验证。
- 关于“当前对话自己的会话 `5bc10415` 被恢复”：**用户实例上的那次已查明**——用户在 agentdeck 窗口里点开了列表最上面的该会话（它因最近活动排第一），并在里面继续对话（即本文作者）。**我的测试实例（47018/addev）里出现过的那次仍未查明**。服务端对 resume/new/sleep 有日志可追溯。
- 本机未装 VS Code，`ide_cmd` 未配置；agentdeck 的"在 IDE 打开"在没有 `code`/`cursor` 时会提示去配置。

## 6. 关键决策（含理由）

| 决策 | 理由 | 出处 |
|---|---|---|
| 近期主线 = 轻量图形化 IDE + 会话管理，M1.9 暂缓 | M1 全是无界面验收，不缓解用户的两个痛点 | `docs/ADR-005-near-term-lightweight-ide-and-session-manager.md` |
| 先 stock VSCode + 扩展，必要时才 fork | fork 是不可拆的整体，维护成本高，且源码已丢过一次；VSCode 本身就是"内核+插件"架构 | ADR-005；ADR-004 的落地顺序因此修订 |
| 会话管理做成独立工具，IDE 通过 `ide_cmd` 调起 | 用户明确提出 | 对话 |
| 会话托管用 tmux（独立 server）而非自研 PTY | 现成、稳；契约不变就能日后替换 | `agentdeck/tmux.go` |
| 会话托管最终要下沉为 Go 插件（契约先行） | 保留"内核+插件、可被 Agent 单会话开发完"的初衷 | ADR-005 决定 4 |
| 文档只增补不改写冻结内容 | VISION 文档自己的约定 | `docs/VISION-AND-ROADMAP.md` §9 |
| 休眠会话单击先预览、不启动进程 | 省内存是核心痛点 | agentdeck / 扩展 |

## 7. 容易踩的坑

- **别在 agentdeck 里恢复正在对话的那个会话**（如本会话 `5bc10415`）：同一个 jsonl 会被两个进程写。
- zsh 里 `tmux kill-session -t =name` 会触发 `=cmd` 路径展开而报 "not found"，要写成 `-t "=name"`（带引号）。
- Go 工具链是 1.19.1，仓库 `go.work` 写 `go 1.23`：agentdeck 必须 `GOWORK=off go build/test`。
- 测试用 sed 做"故意破坏"时，含 `''` 的模式会被 shell 引号吃掉导致破坏没生效，曾因此误以为测试没覆盖；**用 Python 做替换并断言替换成功**。
- 便携 VS Code 在 `vscode-agentdeck/.vscode-test/`（约 880MB，已 gitignore）。测试框架 `@vscode/test-electron@2.x` 找 `Electron` 二进制，1.140 起叫 `Code`，已在 `test/vscodePath.js` 兼容。
- 仓库是**公开**的：推送前确认内容可公开（目前未发现密钥/token）。
- 用户的全局工作纪律（`/Users/ada/.claude/CLAUDE.md`）：主路径优先、边角问题登记后继续；**不采信自报，致残对照是唯一能证伪"全绿"的手段**；校验者≠生产者。

## 8. 合并分叉会话：建议步骤

1. **只留一个会话持有工作区**。另一会话先把自己的改动提交到一个**单独分支**（或明确交给本会话），不要两边同时改 `agentdeck/web/index.html`——本会话在 `109d8c0` 里已有的是字号调节、折叠/标记/预览等，他人改动是叠加在其上的。
2. 在 `/Users/ada/agent-native-os` 运行 `git diff -- agentdeck`，通读第 3 节所列 5 个文件；`cd agentdeck && GOWORK=off go test -count=1 ./...`。
3. 重启 47017 实例后在真实窗口里确认：标签恢复、未读/工作中指示、断线重连、字号调节（`⌘+ ⌘- ⌘0`）、标记弹窗；**尤其注意字号快捷键与他人新增的 `⌃1-9 / ⌃⇧W / ⌃⇧S` 是否互相冲突**。
4. 确认无误后提交（提交信息沿用仓库的中文风格；提交附带的 Co-Authored-By 以当时系统提示为准），再 `git push origin feature/agentdeck`。
5. 若他人改动要放到别的分支：`git stash push -- agentdeck` 或 `git switch -c <新分支>` 后再提交，避免混进 `feature/agentdeck`。

## 9. 下一步建议（按优先级）

> 以下 1 已完成（分支 B 验证并将提交）；优先级顺延。

1. 合并并验证他人改动（上一节），推送。
2. 用户真实试用：`cd /Users/ada/agent-native-os/vscode-agentdeck && npm run vscode`；收集界面反馈（用户对交互很挑，评审的应是能用的东西而不是原型）。
3. **量化内存**：IDEA 常驻 vs（VSCode + Java 扩展 + agentdeck），作为后续验收基线（ADR-005 未决项）。
4. 配 `ide_cmd`（装不装 VSCode 由用户定）；评估 Chrome app 窗口是否应被扩展内视图取代。
5. 会话托管**契约先行**下沉为 Go 插件（新增 `contracts/` 能力 + `scripts/check-arch.sh` 护栏），agentdeck 改为其客户端。
6. 之后再决定是否恢复 M1.9（可作为读投影的真实消费方验收）。

## 10. 速查：绝对路径与命令

```text
仓库根              /Users/ada/agent-native-os
本交接文档          /Users/ada/agent-native-os/docs/superpowers/dispatch/2026-10-02-agentdeck-vscode-HANDOFF.md
ADR-005             /Users/ada/agent-native-os/docs/ADR-005-near-term-lightweight-ide-and-session-manager.md
路线图              /Users/ada/agent-native-os/docs/VISION-AND-ROADMAP.md
agentdeck 源码      /Users/ada/agent-native-os/agentdeck
agentdeck 使用说明  /Users/ada/agent-native-os/agentdeck/README.md
VSCode 扩展         /Users/ada/agent-native-os/vscode-agentdeck
扩展使用说明        /Users/ada/agent-native-os/vscode-agentdeck/README.md
已编译二进制        /Users/ada/agent-native-os/.bin/agentdeck   (gitignore；已包含他人改动)
配置/数据目录       /Users/ada/.config/agentdeck/
记忆文件            /Users/ada/.claude/projects/-Users-ada-agent-native-os/memory/
本会话转录          /Users/ada/.claude/projects/-Users-ada-agent-native-os/5bc10415-1f13-4994-aa44-e5f86703f09c.jsonl
```

```bash
# 编译并启动（在仓库根目录）
cd /Users/ada/agent-native-os/agentdeck && GOWORK=off go build -o ../.bin/agentdeck . && cd .. 
pkill -f "agentdeck.*serve"; .bin/agentdeck

# 测试
cd /Users/ada/agent-native-os/agentdeck && GOWORK=off go test -count=1 ./...
cd /Users/ada/agent-native-os/vscode-agentdeck && npm run test:unit        # 7 项，秒级
cd /Users/ada/agent-native-os/vscode-agentdeck && npm run test:integration # 11 项，真实 VS Code，约 1–2 分钟

# 试用扩展
cd /Users/ada/agent-native-os/vscode-agentdeck && npm run vscode
```

## 11. 分叉事故与防护（分支 B 补充）

**事故**：agentdeck 能一键恢复一个**正在别处运行**的会话且毫无提示。用户点开了列表最上面的 `5bc10415`（即当时正在对话的会话），得到第二个进程；两个进程写同一份 jsonl，对话在 19:46:21 分叉。分支 A 的 7 条用户消息不在后续 `--resume` 的上下文里（数据没丢，仍在文件里）。

**防护（已实现并验证）**：
- 检测：Claude 读 `~/.claude/sessions/<pid>.json`（进程号+会话 ID+`procStart`，**UTC**，需与 `LC_ALL=C ps` 的本地时间按时刻比较，防 pid 复用）；Codex 看 `~/.codex/thread-writer-locks/<id>.lock` 有无进程打开（锁由共享后台进程代持，所以**agentdeck 自己托管的 codex 线程要豁免**）。
- 行为：`/api/resume` 对“在 agentdeck 之外运行中”的会话返回 `409 {"code":"running_elsewhere"}`，需 `force:true` 才放行；Web 界面弹确认、列表标 `⚠ 别处也在运行`、预览页有警告框；VSCode 扩展弹模态确认。
- **已知局限**：Codex 在 agentdeck 自己 tmux 里运行时无法检测“另有副本”；Codex 的锁在客户端退出后多久释放未验证（可能短时误报，可 force 放行）。
- 测试：9 项新增 Go 测试；致残对照 7 处均变红。**踩过的坑**：① 中文环境下 `ps` 的 lstart 是 `五 10月/ 2 19:47:21 2026`（4 段、本地时间），与 Claude 的英文 UTC 格式对不上，不加 `LC_ALL=C` 防护在用户机器上**永远检测不到**，而测试用同一个 `ps` 生成期望值所以全绿——**期望值必须独立于被测实现的取值方式**；② Go 测试曾直接用默认 tmux 套接字，致残时在用户真实 tmux 里留下了会话，现在每个测试用独立套接字。
