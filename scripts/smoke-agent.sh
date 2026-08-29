#!/usr/bin/env bash
# M1.3 smoke fragment: run a mock agent session against a real worktree, verify the
# AgentRun goes terminal, the transcript blob resolves, the workspace file changed,
# and the run survives a kernel restart.
set -euo pipefail
V=".bin/vibe -socket $SOCK -identity local-cli -token $TOKEN"
RAW=".bin/vibe-raw -socket $SOCK -identity local-cli -token $TOKEN"

SRC="$DATA/agentsrc"
mkdir -p "$SRC"
git -C "$SRC" -c init.defaultBranch=main init -q
echo base > "$SRC/App.java"
git -C "$SRC" add -A
git -C "$SRC" -c user.email=smoke@test -c user.name=smoke -c commit.gpgsign=false commit -q -m init

WC_ID="$($V task create -title "agent smoke" -goal g -repo "$SRC" | sed -n 's/.*wc \([^ ]*\).*/\1/p')"
WT="$($V workspace allocate "$WC_ID" -repo "$SRC" | sed -n 's/.*path \([^ ]*\).*/\1/p')"
[ -d "$WT" ] || { echo "FAIL: no worktree"; exit 1; }

run_out="$($V agent run "$WC_ID" -workspace "$WT" -prompt "touch App" -steps 3 -write-file App.java -write-content '// agent was here
')"
RUN_ID="$(echo "$run_out" | sed -n 's/.*agent_run \([^ ]*\).*/\1/p; s/.*run \([^ ]*\).*/\1/p' | head -1)"
[ -n "$RUN_ID" ] || { echo "FAIL: no run id: $run_out"; exit 1; }

for _ in $(seq 1 50); do
  st="$($V agent show "$RUN_ID" | sed -n 's/.*status \([A-Z]*\).*/\1/p')"
  [ "$st" = "COMPLETED" ] && break
  sleep 0.1
done
[ "$st" = "COMPLETED" ] || { echo "FAIL: run status = $st"; cat "$DATA/kernel.log"; exit 1; }

REF="$($V agent show "$RUN_ID" -json | sed -n 's/.*"raw_session_ref":"\([^"]*\)".*/\1/p')"
echo "$REF" | grep -q '^blob://sha256/' || { echo "FAIL: raw_session_ref = $REF"; exit 1; }
$RAW -cap blob.get -kind query -service default-blob -authority blob-main -payload "{\"uri\":\"$REF\"}" | grep -q 'content_base64' \
  || { echo "FAIL: transcript blob not resolvable"; exit 1; }

grep -q 'agent was here' "$WT/App.java" || { echo "FAIL: mock agent did not change the worktree file"; exit 1; }

restart_kernel
run_back=""
for _ in $(seq 1 50); do
  run_back="$($V agent show "$RUN_ID" 2>/dev/null)"
  echo "$run_back" | grep -q 'COMPLETED' && break
  sleep 0.1
done
echo "$run_back" | grep -q 'COMPLETED' || { echo "FAIL: run lost on restart: $run_back"; cat "$DATA/kernel.log"; exit 1; }

echo "M1.3 AGENT SMOKE: OK"
