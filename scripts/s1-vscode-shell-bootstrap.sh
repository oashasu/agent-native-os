#!/usr/bin/env bash
# S1 VSCode-OSS 壳改造 · 环境门槛 + 构建引导（与 UI-1 spec 附录 A/D 同判据，2026-09-19 更新：brew clang 绕行）
# 用法：
#   ./scripts/s1-vscode-shell-bootstrap.sh            # 环境检查 + 构建
#   ./scripts/s1-vscode-shell-bootstrap.sh --check    # 只检查环境，不构建
# 退出码：0=绿并完成构建 1=检查不过 2=环境阻塞（打印阻塞点）
set -uo pipefail

VS_SRC="${VS_SRC:-/tmp/vscode-ui1}"        # 已有浅克隆可复用
NPM_CACHE="${NPM_CACHE:-/tmp/npm-cache}"   # 权限坑：不要用 ~/.npm
HC_HOME="${HC_HOME:-/tmp/hc-home}"         # 权限坑：node-gyp 缓存目录
HC_BIN="${HC_BIN:-/tmp/hc-bin}"            # brew clang PATH 包装目录
BREW_LLVM="${BREW_LLVM:-/opt/homebrew/opt/llvm/bin}"   # Homebrew 独立 LLVM 工具链

say(){ printf '\033[36m[s1]\033[0m %s\n' "$*"; }
die(){ printf '\033[31m[s1][阻塞]\033[0m %s\n' "$*"; exit 2; }

# ── 0) 工具链门槛 ──
say "== 环境检查 =="
NODE_V=$(node -v 2>/dev/null || echo none)
NPM_V=$(npm -v 2>/dev/null || echo none)
CLANG_RAW=$(clang --version 2>/dev/null | head -1)
CLANG_V=$(echo "$CLANG_RAW" | sed -E 's/.*version ([0-9.]+).*/\1/')
say "node ${NODE_V} / npm ${NPM_V}"
say "clang ${CLANG_RAW:-缺失}"

[[ "$NODE_V" == none ]] && die "缺少 node（仓库要求 ≥ 20；.nvmrc 为 24.x）"
[[ "$NPM_V" == none ]] && die "缺少 npm（上游已弃用 yarn，必须用 npm）"

# 编译器策略：系统 clang ≥ 15 直接用；否则尝试 Homebrew LLVM
COMPILER="system"
if [[ "$CLANG_V" =~ ^[0-9]+(\.[0-9]+)*$ ]]; then
  CLANG_N=${CLANG_V%%.*}
  if [[ "$CLANG_N" -lt 15 ]]; then
    if [[ -x "$BREW_LLVM/clang++" ]]; then
      BREW_V=$("$BREW_LLVM/clang++" --version | head -1)
      say "系统 clang ${CLANG_V} < 15；改用 Homebrew LLVM：$BREW_V"
      COMPILER="brew"
    else
      die "clang ${CLANG_V} < 15 且无 Homebrew LLVM（$BREW_LLVM 不存在）。需要 Xcode ≥ 15 / CLT ≥ 15，或安装 llvm（brew install llvm）。本机路径见 UI-1 spec 附录 D 更新。"
    fi
  fi
else
  say "clang 版本无法解析（${CLANG_RAW:-无}），按系统编译器继续（安装期若 gyp 报 source_location 再按 brew 路径处理）"
fi

say "== 门槛通过：node ≥ 20；编译器 = ${COMPILER} =="

if [[ "${1:-}" == "--check" ]]; then
  [[ "$COMPILER" == "brew" ]] && say "提示：构建将把 $BREW_LLVM 经 PATH 包装为 cc/c++/gcc/g++ 置于首位（node-gyp 按命令名解析编译器；npm≥11 已废弃 npm_config_cc/cxx）。"
  say "仅检查模式：环境绿，未构建。"
  exit 0
fi

# ── 0.5) 编译器 PATH 包装（brew 路径时） ──
if [[ "$COMPILER" == "brew" ]]; then
  mkdir -p "$HC_BIN"
  for t in clang clang++; do ln -sf "$BREW_LLVM/$t" "$HC_BIN/$t" 2>/dev/null; done
  ln -sf clang  "$HC_BIN/cc" 2>/dev/null
  ln -sf clang++ "$HC_BIN/c++" 2>/dev/null
  ln -sf clang  "$HC_BIN/gcc" 2>/dev/null
  ln -sf clang++ "$HC_BIN/g++" 2>/dev/null
  export PATH="$HC_BIN:$PATH"
  say "编译器包装就绪：$(c++ --version | head -1)"
fi

# ── 1) 源码就位 ──
if [[ ! -d "$VS_SRC/.git" ]]; then
  say "clone Code-OSS（浅）……"
  git clone --depth 1 https://github.com/microsoft/vscode.git "$VS_SRC" || die "clone 失败（网络/权限）"
else
  say "复用已有克隆 $VS_SRC @ $(git -C "$VS_SRC" rev-parse --short HEAD 2>/dev/null)"
fi

# ── 2) 依赖安装（已知坑逐一规避，见 spec 附录 A） ──
cd "$VS_SRC"
export npm_config_cache="$NPM_CACHE"
export HOME="$HC_HOME"
export VSCODE_SKIP_NODE_VERSION_CHECK=1   # 本机 node 版本与 .nvmrc 不必严格一致
say "npm install（preinstall 会拒绝 yarn；缓存 $NPM_CACHE）……"
npm install || die "npm install 失败（见日志；若 ENOTEMPTY 风暴 → rm -rf node_modules 后重跑）"

# ── 3) dev watch（前台） ──
say "npm run watch（Ctrl-C 停止）……"
exec npm run watch