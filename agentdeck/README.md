# agentdeck — claude / codex 会话管理器（独立工具）

解决：会话开太多占内存、iTerm2 误关丢会话、找回/切换不方便。

## 用法

```bash
bash scripts/build.sh   # 或：cd agentdeck && GOWORK=off go build -o ../.bin/agentdeck .
.bin/agentdeck          # 启动（后台服务 + Chrome app 窗口）
.bin/agentdeck list     # 命令行列出所有会话（● = 运行中）
```

- 左侧：`~/.claude`、`~/.codex` 里的全部历史会话，按项目目录分组；绿点 = 运行中，右侧显示该会话进程树内存。
- 双击 / 回车：恢复（`claude --resume` / `codex resume`）并接入终端。
- **关闭窗口、杀掉服务都不会中断会话**——agent 跑在独立的 tmux server（`tmux -L agentdeck`）里。
- **休眠**：结束进程释放内存，会话保留，随时恢复。
- 快捷键：`⌘K` 搜索，`↑↓` 选择，`↵` 打开，`⌘[` / `⌘]` 切标签。
- 终端里 tmux 开了鼠标模式：滚轮滚动历史；要选中复制请按住 Option 拖选。

## 配置 `~/.config/agentdeck/config.json`（可选）

```json
{
  "claude_args": ["--dangerously-skip-permissions"],
  "codex_args": ["--yolo"],
  "ide_cmd": "code {cwd}"
}
```

别名（alias）在非交互环境里不生效，所以需要的参数写在这里。`ide_cmd` 未配置时依次尝试 `code`、`cursor`。

## 安全

服务只监听 `127.0.0.1`；终端等同本机 shell，因此：Host 校验（防 DNS rebinding）+ 随机 token cookie + WebSocket 同源校验 + POST 自定义头。token 在 `~/.config/agentdeck/token`（0600）。

## 测试

`cd agentdeck && GOWORK=off go test ./...`（索引解析、子代理过滤、WebSocket 桥接、断开后会话存活、鉴权）。
