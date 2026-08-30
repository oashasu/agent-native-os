#!/usr/bin/env bash
# M1.4 smoke fragment: collect a worktree diff, run a passing + a failing tool
# command, verify evidence + blob refs + fingerprint, and survive a kernel restart.
# Assumes SOCK/DATA/TOKEN exported and restart_kernel available.
set -euo pipefail
V=".bin/vibe -socket $SOCK -identity m1-dev -token $DEV_TOKEN"
RAW=".bin/vibe-raw -socket $SOCK -identity m1-dev -token $DEV_TOKEN"

SRC="$DATA/artsrc"
mkdir -p "$SRC"
git -C "$SRC" -c init.defaultBranch=main init -q
printf 'class Calc { int add(int a,int b){return a+b;} }\n' > "$SRC/Calc.java"
git -C "$SRC" add -A
git -C "$SRC" -c user.email=s@t -c user.name=s -c commit.gpgsign=false commit -q -m init

WC_ID="$($V task create -title "art smoke" -goal g -repo "$SRC" | sed -n 's/.*wc \([^ ]*\).*/\1/p')"
WT="$($V workspace allocate "$WC_ID" -repo "$SRC" | sed -n 's/.*path \([^ ]*\).*/\1/p')"
$V agent run "$WC_ID" -workspace "$WT" -prompt "harden add" -steps 2 -write-file Calc.java -write-content '// hardened by agent
' >/dev/null
sleep 0.5

# --- collect diff ---
diff_out="$($V artifact collect-diff "$WC_ID" -workspace "$WT")"
ART_ID="$(echo "$diff_out" | sed -n 's/^artifact \([^ ]*\).*/\1/p')"
echo "$diff_out" | grep -qE 'files_changed [1-9]' || { echo "FAIL: no diff detected: $diff_out"; exit 1; }
DIFF_BLOB="$(echo "$diff_out" | sed -n 's/.*blob \([^ ]*\).*/\1/p')"
$RAW -cap blob.get -kind query -service default-blob -authority blob-main -payload "{\"uri\":\"$DIFF_BLOB\"}" | grep -q content_base64 \
  || { echo "FAIL: diff blob not resolvable"; exit 1; }

# --- passing build ---
build_out="$($V tool run "$WC_ID" -workspace "$WT" -label build -- sh -c 'echo compiling; exit 0')"
echo "$build_out" | grep -q 'outcome PASS' || { echo "FAIL: build: $build_out"; exit 1; }
echo "$build_out" | grep -qE 'fp [0-9a-f]{12}' || { echo "FAIL: no fingerprint: $build_out"; exit 1; }
BUILD_TR="$(echo "$build_out" | sed -n 's/^tool_run \([^ ]*\).*/\1/p')"
SOUT="$($V tool show "$BUILD_TR" -json | sed -n 's/.*"stdout_uri":"\([^"]*\)".*/\1/p')"
$RAW -cap blob.get -kind query -service default-blob -authority blob-main -payload "{\"uri\":\"$SOUT\"}" \
  | python3 -c 'import sys,json,base64; d=json.load(sys.stdin); print(base64.b64decode(d["content_base64"]).decode())' | grep -q compiling \
  || { echo "FAIL: build stdout blob missing 'compiling'"; exit 1; }

# --- failing test ---
test_out="$($V tool run "$WC_ID" -workspace "$WT" -label test -- sh -c 'echo running; exit 1')"
echo "$test_out" | grep -q 'outcome FAIL' || { echo "FAIL: failing test not FAIL: $test_out"; exit 1; }
echo "$test_out" | grep -q 'exit 1' || { echo "FAIL: exit code not 1: $test_out"; exit 1; }

restart_kernel
art_back=""
for _ in $(seq 1 100); do
  art_back="$($V artifact show "$ART_ID" 2>/dev/null)"
  echo "$art_back" | grep -q 'diff' && break
  sleep 0.1
done
echo "$art_back" | grep -q 'diff' || { echo "FAIL: artifact lost on restart: $art_back"; cat "$DATA/kernel.log"; exit 1; }
tr_back=""
for _ in $(seq 1 100); do
  tr_back="$($V tool show "$BUILD_TR" 2>/dev/null)"
  echo "$tr_back" | grep -q 'PASS' && break
  sleep 0.1
done
echo "$tr_back" | grep -q 'PASS' || { echo "FAIL: tool run lost on restart: $tr_back"; exit 1; }

echo "M1.4 ARTIFACT+TOOL SMOKE: OK"
