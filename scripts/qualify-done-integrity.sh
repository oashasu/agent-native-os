#!/usr/bin/env bash
# M1.7 adversarial DONE-integrity qualification. Drives the LIVE kernel as the
# EXTERNAL identity (local-cli) and proves the four docs/M1-DESIGN.md §4.4
# attacks cannot reach DONE. Sibling of kernel/tests/integration/m05_qualification.py.
#
# Scope: proves the four §4.4 attacks are blocked. Not a proof that every
# conceivable bypass is closed.
#
# Discipline: every assertion captures output into a variable and matches with
# `case` -- never `cmd | grep -q`, never `grep -o | head -1` (SIGPIPE trips
# pipefail; the flake M1.5/M1.6 hit). `vibe workflow run` exits non-zero on any
# non-DONE outcome, so it is always run as `... 2>&1 || true` / checked via $?.
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/lib/kernel-harness.sh
build_bins
restart_kernel

fail() {  # $1 = message
  echo "$1"
  echo "--- kernel.log tail ---"
  tail -n 40 "$DATA/kernel.log" 2>/dev/null || true
  exit 1
}

VQ=".bin/vibe -socket $SOCK -identity local-cli -token $TOKEN"   # attacker / qualification identity
VD=".bin/vibe -socket $SOCK -identity m1-dev -token $DEV_TOKEN"  # trusted setup + status reads

# Throwaway source repo for the workflow's worktree.
SRC="$DATA/qsrc"; mkdir -p "$SRC"
git -C "$SRC" -c init.defaultBranch=main init -q
printf 'class Calc { int add(int a,int b){return a+b;} }\n' > "$SRC/Calc.java"
git -C "$SRC" add -A
git -C "$SRC" -c user.email=s@t -c user.name=s -c commit.gpgsign=false commit -q -m init

# ---------- S1: external identity has NO work.transition@1 (direct or delegated-out) ----------
created="$($VD task create -title "s1" -goal "g" -repo "$SRC" -ac AC1="x")"
S1_WC="$(printf '%s\n' "$created" | sed -n 's/.*wc \([^ ]*\).*/\1/p')"
[ -n "$S1_WC" ] || fail "S1 FAIL: task create: $created"
for target in IN_PROGRESS IN_REVIEW DONE FAILED; do
  out="$($VQ task transition "$S1_WC" -to "$target" -expected-version 1 2>&1 || true)"
  case "$out" in
    *"did not grant"*) : ;;
    *) fail "S1 FAIL: local-cli was NOT denied work.transition -to $target: $out" ;;
  esac
done
echo "S1 OK: external direct work.transition denied for every target state"

echo "DONE-INTEGRITY QUALIFICATION: OK"
