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

# ---------- S2: failed evidence cannot pass the DONE gate ----------
# Run the workflow with a failing build or test, approve the review anyway
# (a colluding human), assert the gate still refuses DONE.
s2_run() {  # $1=build cmd  $2=test cmd  $3=expected reason substring
  local created task show j pid rev decide wf_out ts
  created="$($VD task create -title "s2" -goal "harden add" -repo "$SRC" -ac AC1="build+test pass")"
  task="$(printf '%s\n' "$created" | sed -n 's/^task \([^ ]*\).*/\1/p')"
  [ -n "$task" ] || fail "S2 FAIL: task create: $created"
  ( $VQ workflow run "$task" -prompt "harden" -build "$1" -test "$2" \
      -review-poll-ms 200 -mock-write-file Calc.java -mock-write-content '// s2
' -timeout 3m > "$DATA/s2.out" 2>&1 ) &
  pid=$!
  rev=""
  for _ in $(seq 1 600); do
    kill -0 "$pid" 2>/dev/null || break
    show="$($VQ workflow show "$task" 2>/dev/null || true)"
    case "$show" in *"stage WAITING_REVIEW"*)
      j="$($VQ workflow show "$task" -json 2>/dev/null || true)"
      rev="$(printf '%s\n' "$j" | grep -m1 -o '"review_id":"[^"]*"' | sed -n '1{s/.*:"//;s/"//;p;}')"
      [ -n "$rev" ] && break ;;
    esac
    sleep 0.1
  done
  [ -n "$rev" ] || { cat "$DATA/s2.out"; fail "S2 FAIL: never reached WAITING_REVIEW ($1 / $2)"; }
  decide="$($VQ review decide "$rev" -approved -reviewer mallory -acceptance AC1=pass 2>&1 || true)"
  case "$decide" in *"status APPROVED"*) : ;; *) fail "S2 FAIL: review decide: $decide" ;; esac
  if wait "$pid"; then cat "$DATA/s2.out"; fail "S2 FAIL: workflow run exited 0 with $1 / $2"; fi
  wf_out="$(cat "$DATA/s2.out")"
  case "$wf_out" in *"outcome GATE_FAILED"*) : ;; *) fail "S2 FAIL: outcome not GATE_FAILED ($1 / $2): $wf_out" ;; esac
  case "$wf_out" in *"$3"*) : ;; *) fail "S2 FAIL: reason lacks '$3': $wf_out" ;; esac
  ts="$($VD task show "$task" 2>&1 || true)"
  case "$ts" in *"status DONE"*) fail "S2 FAIL: task went DONE despite $1 / $2: $ts" ;; esac
  restart_kernel
  ts="$($VD task show "$task" 2>&1 || true)"
  case "$ts" in *"status DONE"*) fail "S2 FAIL: task DONE after restart: $ts" ;; esac
}
s2_run "sh -c true"  "sh -c false" "reason test:"
s2_run "sh -c false" "sh -c true"  "reason build:"
echo "S2 OK: failing test and failing build both blocked at the gate"

echo "DONE-INTEGRITY QUALIFICATION: OK"
