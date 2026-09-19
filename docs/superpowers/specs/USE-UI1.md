# Human Console（UI-1）使用手册

> 这不是设计文档。这是「怎么把它跑起来、怎么用它、怎么验收」的操作手册。
> 设计细节在 `2026-09-19-ui-1-vscode-fork-shell-design.md`（附录 A–H），遇到报错再翻。

## 0. 这堆东西到底是什么

| 文件/目录 | 是什么 | 你需要做什么 |
|---|---|---|
| `scripts/s1-vscode-shell-bootstrap.sh` | **一键构建脚本**（环境检查 → 装依赖 → 编译 → 起 dev 窗口） | **就跑它** |
| `docs/superpowers/specs/2026-09-19-ui-1-vscode-fork-shell-design.md` | 设计规格（14 个附录：判据、验收、故障手册） | 报错/验收时查 |
| `docs/superpowers/specs/2026-09-19-ui-1-s1-shell-patches/` | S1 命令骨架源代码（已打进 fork，**不用你动手**） | 不用管 |
| `prototypes/human-console.html` | 交互原型（浏览器直接打开 `open prototypes/human-console.html`） | 想先看长啥样就开它 |
| `/tmp/vscode-ui1/` | VSCode-OSS fork 克隆（已改好、已编译） | 机器上自动重建，不用管 |

一句话：**你只需要跑一个脚本，然后按快捷键。**

## 1. 跑起来（需要一台有显示器的 mac：Xcode ≥ 15 或 `brew install llvm`）

```bash
# 在本仓库目录下
./scripts/s1-vscode-shell-bootstrap.sh
```

脚本会依次：检查环境 → （必要时用 Homebrew LLVM 绕过 C++20 编译死锁）→ 准备源码 → `npm install` → `npm run watch`（编译并起 Human Console dev 窗口，会一直挂着，别关）。

> **安装包打开后看到普通 VSCode？** 那是旧的 dev 版/旧包。**新包（2026-09-19 21:22 后产出的 zip）首启直接进 Agent 镜头**（左树+对话流+Changes/Review），按 `⌃1` 才是 IDE 镜头。若仍看到纯 VSCode 外观，确认装的是 `/tmp/human-console-20260919.zip` 的新包。

- 只想检查环境不构建：`./scripts/s1-vscode-shell-bootstrap.sh --check`
- 构建在自己机器从头来约需一次性的 10–30 分钟（首次 npm install + 编译），之后增量很快
- 换机器/重来：删掉 `/tmp/vscode-ui1` 再跑即可

## 1.5 要安装包（不用 dev 窗口，双击即用）

```bash
./scripts/s1-package-ui1-app.sh
```

产物：
- `/tmp/VSCode-darwin-arm64/Human Console.app` —— 双击即用，可拖进 /Applications
- `/tmp/human-console-<日期>.zip` —— 分发/拷到别的 mac 用

安装提示：产物未签名，首次打开 macOS 会拦——右键 → 打开 → 再点「打开」，或 `xattr -dr com.apple.quarantine /Applications/Human\ Console.app`。
（要正式签名/公证 / dmg 安装界面：需 Apple Developer 证书，属发布流程，不在本仓库范围。）

## 2. 打开之后怎么操作

| 按键 | 作用 |
|---|---|
| `⌃1` | **IDE 镜头**（正常编码：编辑器、侧边栏、终端，全部原生 VSCode） |
| `⌃2` | **Agent 镜头**（左：工作区/会话导航树；右：对话流 / Changes / Review） |
| `F4` | 两镜头互斥切换 |
| `⌃K` | 换根：弹出工作区列表，回车切换（树和对话整体切到那个工作区） |
| `[` / `]` | Agent 镜头下：上/下一个会话 |
| `a` | Agent 镜头下：批准当前会话第一个待审批的工具调用 |
| `r` | 拒绝（预留，等真通道） |
| 鼠标 | Agent 镜头里点树切会话；**双击 Changes 里的文件 → 编辑器直接打开并跳到改动行** |

## 3. 怎么验收（对照检查）

- [ ] 窗口标题/菜单是 **Human Console**
- [ ] `⌃1`/`⌃2` 切换时活动栏/状态栏隐藏还原，编辑器组不丢
- [ ] Agent 镜头左树：3 个工作区（支付服务/批量核对/JPA 审计），点开有会话，状态灯绿/黄
- [ ] 对话流里能看到卡片（规划/工具/编辑/测试…），工具卡带 Bash 标签和「批准一次/始终允许」按钮
- [ ] Changes 面板有文件行，单击展开 diff，双击跳进真实编辑器到改动行
- [ ] `⌃K` 换到别的根，三块面板内容跟着换
- （验收依据完整版：spec 附录 G/H）

## 4. 当前边界（诚实说明）

- 对话数据是**内置 mock**（原型 t17/t23/t31），还没接真 agent 进程 —— 界面、树、审批按钮全是真的，但背后没有真执行
- IDE 侧是**原生 VSCode**：编辑/终端/git 全部原样，没改（这是设计：IDE 复用）
- 无显示器的机器（比如本沙箱）只能验证到「编译零错误 + 产物齐全」，窗口交互必须你在有屏机器上验

## 5. 常见问题

| 症状 | 处理 |
|---|---|
| 编译报 `source_location` 找不到 | 系统 clang < 15，装 `brew install llvm`（脚本会自动用） |
| Electron 下载失败/慢 | 换网或代理后再跑脚本（会续传） |
| `npm install` 报 ENOTEMPTY 风暴 | `rm -rf /tmp/vscode-ui1/node_modules` 重跑 |
| dev 窗口打不开但编译过了 | 检查是否有显示环境；按 `npm run electron` 补齐运行时 |
| 想重置 | `rm -rf /tmp/vscode-ui1 /tmp/hc-home /tmp/npm-cache /tmp/hc-bin` 后重跑 |

## 6. 想继续往下做

下一步是给 Agent 接**真 spine 通道**（`docs/superpowers/specs/2026-09-19-ui-1-vscode-fork-shell-design.md` 附录 C.3 的 `SpineProvider` 接口 + 附录 E 的消息协议已就位，换实现即可）。代码侧验收通过后说一声即可推进。