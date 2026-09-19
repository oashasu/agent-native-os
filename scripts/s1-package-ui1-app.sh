#!/usr/bin/env bash
# 打出可双击的 Human Console.app + ZIP 分发包（UI-1 交付物）
# 前置：scripts/s1-vscode-shell-bootstrap.sh 已跑通一次（/tmp/vscode-ui1 已装好依赖）
# 用法：
#   ./scripts/s1-package-ui1-app.sh            # 生产打包 → .app + .zip
#   ./scripts/s1-package-ui1-app.sh --zip-only # 已有 .app，只打 zip
# 产物：
#   /tmp/VSCode-darwin-arm64/Human Console.app   （双击即用，可拖进 /Applications）
#   /tmp/human-console-<日期>.zip                （分发/拷贝到别的 mac）
set -uo pipefail

VS_SRC="${VS_SRC:-/tmp/vscode-ui1}"
DEST_ROOT="${DEST_ROOT:-/tmp}"
NPM_CACHE="${NPM_CACHE:-/tmp/npm-cache}"
HC_HOME="${HC_HOME:-/tmp/hc-home}"

say(){ printf '\033[36m[s1pkg]\033[0m %s\n' "$*"; }
die(){ printf '\033[31m[s1pkg][阻塞]\033[0m %s\n' "$*"; exit 2; }

if [[ -z "${VSCODE_SKIP_NODE_VERSION_CHECK:-}" ]]; then
  export VSCODE_SKIP_NODE_VERSION_CHECK=1
fi
export HOME="$HC_HOME" npm_config_cache="$NPM_CACHE" PATH="/tmp/hc-bin:$PATH"

ZIP_NAME="human-console-$(date +%Y%m%d).zip"
APP="$DEST_ROOT/VSCode-darwin-arm64/Human Console.app"

if [[ "${1:-}" == "--zip-only" ]]; then
  [[ -d "$APP" ]] || die "没有现成 .app：$APP"
else
  [[ -d "$VS_SRC/.git" ]] || die "源码不在 $VS_SRC（先跑 s1-vscode-shell-bootstrap.sh）"
  cd "$VS_SRC"
  say "gulp vscode-darwin-arm64（生产打包：extensions + esbuild bundle + .app）……"
  npx gulp vscode-darwin-arm64 || die "打包失败，看日志"
  [[ -d "$APP" ]] || die "产物缺失：$APP"
fi

say "打 zip：$DEST_ROOT/$ZIP_NAME"
( cd "$DEST_ROOT/VSCode-darwin-arm64" && zip -ryq "$DEST_ROOT/$ZIP_NAME" "$(basename "$APP")" ) || die "zip 失败"
say "完成："
say "  $APP"
say "  $DEST_ROOT/$ZIP_NAME"
say "安装：解压 zip 或把 .app 拖进 /Applications，双击打开（首次 macOS 右键→打开 绕过未签名提示）。"