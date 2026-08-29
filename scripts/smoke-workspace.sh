#!/usr/bin/env bash
# M1.2 smoke fragment: allocate a worktree for a WorkContext, verify it, survive a
# kernel restart, release with policy=preserve, confirm the worktree is kept.
set -euo pipefail
V=".bin/vibe -socket $SOCK -identity local-cli -token $TOKEN"

SRC="$DATA/srcrepo"
mkdir -p "$SRC"
git -C "$SRC" -c init.defaultBranch=main init -q
echo scratch > "$SRC/README.md"
git -C "$SRC" add -A
git -C "$SRC" -c user.email=smoke@test -c user.name=smoke -c commit.gpgsign=false commit -q -m init
BASE="$(git -C "$SRC" rev-parse HEAD)"

create_out="$($V task create -title "ws smoke" -goal g -repo "$SRC")"
WC_ID="$(echo "$create_out" | sed -n 's/.*wc \([^ ]*\).*/\1/p')"
[ -n "$WC_ID" ] || { echo "FAIL: no wc id from: $create_out"; exit 1; }

alloc_out="$($V workspace allocate "$WC_ID" -repo "$SRC")"
WS_ID="$(echo "$alloc_out" | sed -n 's/^workspace \([^ ]*\).*/\1/p')"
WT_PATH="$(echo "$alloc_out" | sed -n 's/.*path \([^ ]*\).*/\1/p')"
WT_BRANCH="$(echo "$alloc_out" | sed -n 's/.*branch \([^ ]*\).*/\1/p')"
[ -d "$WT_PATH" ] || { echo "FAIL: worktree dir missing: $alloc_out"; exit 1; }
[ "$(git -C "$WT_PATH" rev-parse --abbrev-ref HEAD)" = "$WT_BRANCH" ] || { echo "FAIL: worktree not on $WT_BRANCH"; exit 1; }
[ "$(git -C "$WT_PATH" rev-parse HEAD)" = "$BASE" ] || { echo "FAIL: worktree not at base commit"; exit 1; }

restart_kernel

show_out=""
for _ in $(seq 1 50); do
  show_out="$($V workspace show "$WS_ID" 2>/dev/null)"
  echo "$show_out" | grep -q 'status.*ALLOCATED' && break
  sleep 0.1
done
echo "$show_out" | grep -q "$WS_ID" || { echo "FAIL: workspace lost on restart: $show_out"; exit 1; }
echo "$show_out" | grep -q 'status.*ALLOCATED' || { echo "FAIL: workspace status changed on restart: $show_out"; exit 1; }

$V workspace release "$WS_ID" -policy preserve | grep -q 'RELEASED' || { echo "FAIL: release"; exit 1; }
[ -d "$WT_PATH" ] || { echo "FAIL: preserve policy removed the worktree"; exit 1; }

echo "M1.2 WORKSPACE SMOKE: OK"
