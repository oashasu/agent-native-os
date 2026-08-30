#!/usr/bin/env bash
# M1.6 smoke: the whole locked chain via one `workflow run` (started as the
# qualification identity `local-cli`), a human review decision from a second
# "terminal", ending in a DONE Task + a sealed SessionRecord that survives a
# kernel restart. Also asserts the qualification identity CANNOT call
# work.transition directly (M1.7 preview).
#
# Every check captures command output into a variable and greps the variable —
# never `cmd | grep -q` — so a fast `grep -q` cannot SIGPIPE the producer and
# trip `set -o pipefail` (the class of flake M1.5's Task 8 hit).
set -euo pipefail
VQ=".bin/vibe -socket $SOCK -identity local-cli -token $TOKEN"   # qualification identity
VD=".bin/vibe -socket $SOCK -identity m1-dev -token $DEV_TOKEN"  # dev identity (task create / reads)

SRC="$DATA/wfsrc"; mkdir -p "$SRC"
git -C "$SRC" -c init.defaultBranch=main init -q
printf 'class Calc { int add(int a,int b){return a+b;} }\n' > "$SRC/Calc.java"
git -C "$SRC" add -A
git -C "$SRC" -c user.email=s@t -c user.name=s -c commit.gpgsign=false commit -q -m init

create_out="$($VD task create -title "wf" -goal "harden add" -repo "$SRC" -ac AC1="build+test pass")"
TASK_ID="$(printf '%s\n' "$create_out" | sed -n 's/^task \([^ ]*\).*/\1/p')"
WC_ID="$(printf '%s\n'   "$create_out" | sed -n 's/.*wc \([^ ]*\).*/\1/p')"
[ -n "$TASK_ID" ] && [ -n "$WC_ID" ] || { echo "FAIL: task create output: $create_out"; exit 1; }

# --- M1.7 preview: local-cli has NO direct work.transition@1 grant ---
denied="$($VQ task transition "$WC_ID" -to IN_PROGRESS -expected-version 1 2>&1 || true)"
case "$denied" in
  *"did not grant"*) : ;;
  *) echo "FAIL: local-cli was NOT denied direct work.transition: $denied"; exit 1 ;;
esac

# --- run the workflow in the background; it blocks at WAITING_REVIEW ---
( $VQ workflow run "$TASK_ID" -prompt "harden add" -build "sh -c true" -test "sh -c true" \
    -review-poll-ms 200 -mock-write-file Calc.java -mock-write-content '// hardened
' -timeout 3m > "$DATA/wf.out" 2>&1 ) &
WF_PID=$!

# --- wait for WAITING_REVIEW, then find the PENDING review and decide it ---
REV_ID=""
for _ in $(seq 1 300); do
  kill -0 "$WF_PID" 2>/dev/null || break
  show="$($VQ workflow show "$TASK_ID" 2>/dev/null || true)"
  case "$show" in
    *"stage WAITING_REVIEW"*)
      j="$($VQ workflow show "$TASK_ID" -json 2>/dev/null || true)"
      REV_ID="$(printf '%s\n' "$j" | grep -o '"review_id":"[^"]*"' | head -1 | sed 's/.*:"//;s/"//')"
      [ -n "$REV_ID" ] && break ;;
  esac
  sleep 0.1
done
[ -n "$REV_ID" ] || { echo "FAIL: workflow never reached WAITING_REVIEW with a review id"; cat "$DATA/wf.out"; exit 1; }

decide_out="$($VQ review decide "$REV_ID" -approved -reviewer alice -acceptance AC1=pass 2>&1 || true)"
case "$decide_out" in *APPROVED*) : ;; *) echo "FAIL: review decide: $decide_out"; exit 1 ;; esac

wait "$WF_PID" || { echo "FAIL: workflow run exited non-zero: $(cat "$DATA/wf.out")"; exit 1; }
wf_out="$(cat "$DATA/wf.out")"
case "$wf_out" in *"outcome DONE"*)   : ;; *) echo "FAIL: workflow outcome not DONE: $wf_out"; exit 1 ;; esac
case "$wf_out" in *"session sess-"*)  : ;; *) echo "FAIL: no session record in workflow result: $wf_out"; exit 1 ;; esac

# --- Task is DONE and the derived projection agrees ---
ts="$($VD task show "$TASK_ID" 2>&1 || true)"
case "$ts" in *"status DONE"*) : ;; *) echo "FAIL: task not DONE: $ts"; exit 1 ;; esac
wshow="$($VQ workflow show "$TASK_ID" 2>&1 || true)"
case "$wshow" in *"outcome DONE"*) : ;; *) echo "FAIL: workflow projection outcome not DONE: $wshow"; exit 1 ;; esac

# --- restart survival ---
restart_kernel
ok=""
for _ in $(seq 1 100); do
  ts="$($VD task show "$TASK_ID" 2>/dev/null || true)"
  case "$ts" in *"status DONE"*) ok=1; break ;; esac
  sleep 0.1
done
[ -n "$ok" ] || { echo "FAIL: DONE lost on restart: $ts"; exit 1; }

echo "M1.6 WORKFLOW SMOKE: OK"
