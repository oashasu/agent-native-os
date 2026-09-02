#!/usr/bin/env bash
# M1.8 real-provider check. NOT part of any automated gate. Reviewer runs it
# on a machine with codex installed and authenticated:
#   VIBE_REAL_PROVIDER=codex bash "scripts/verify-real-provider.sh"
set -euo pipefail
[ "${VIBE_REAL_PROVIDER:-}" = "codex" ] || { echo "SKIP: set VIBE_REAL_PROVIDER=codex to run"; exit 0; }
cd "$(dirname "$0")/.."
source "scripts/lib/kernel-harness.sh"
build_bins

command -v codex >/dev/null || { echo "FAIL: codex not on PATH"; exit 1; }

SRC="$DATA/scratch-repo"; mkdir -p "$SRC"
git -C "$SRC" -c init.defaultBranch=main init -q
printf 'class Calc {\n    int add(int a, int b) { return a + b; }\n}\n' > "$SRC/Calc.java"
git -C "$SRC" add -A
git -C "$SRC" -c user.email=test@example.com -c user.name=test -c commit.gpgsign=false commit -q -m init

export VIBE_AGENT_PROVIDERS=codex
restart_kernel

VD=( ".bin/vibe" -socket "$SOCK" -identity "m1-dev" -token "$DEV_TOKEN" )
RAW=( ".bin/vibe-raw" -socket "$SOCK" -identity "m1-dev" -token "$DEV_TOKEN" )
created="$("${VD[@]}" task create -title rp -goal rp -repo "$SRC" -ac AC1=x)"
WC="$(printf '%s\n' "$created" | sed -n 's/.*wc \([^ ]*\).*/\1/p')"
alloc="$("${VD[@]}" workspace allocate "$WC" -repo "$SRC")"
WSPATH="$(printf '%s\n' "$alloc" | sed -n 's/.*path \([^ ]*\).*/\1/p')"
[ -n "$WSPATH" ] || { echo "FAIL: no workspace path: $alloc"; exit 1; }

set +e
run_out="$("${VD[@]}" agent run "$WC" -workspace "$WSPATH" -provider codex -timeout 5m \
  -prompt 'Add a one-line Javadoc above the add method in Calc.java.' 2>&1)"
run_rc=$?
set -e
printf '%s\n' "$run_out"
[ "$run_rc" -eq 0 ] || { echo "FAIL: agent run exited $run_rc"; exit 1; }
case "$run_out" in *"» "*) : ;; *) echo "FAIL: no streamed frames"; exit 1 ;; esac
RUN_ID="$(printf '%s\n' "$run_out" | sed -n 's/^agent_run \([^ ]*\).*/\1/p')"
[ -n "$RUN_ID" ] || { echo "FAIL: no run id"; exit 1; }

q="$("${RAW[@]}" -cap agent.run.query -kind query -service default-agent-harness -authority agent-runs-main \
  -payload "{\"work_context_id\":\"$WC\"}")"
printf '%s\n' "$q"
if ! query_check="$(printf '%s\n' "$q" | python3 -c 'import json,sys; run_id=sys.argv[1]; runs=json.load(sys.stdin).get("agent_runs", []); assert len(runs)==1 and runs[0].get("id")==run_id and runs[0].get("provider")=="codex"; print("OK")' "$RUN_ID")"; then
  echo "FAIL: agent.run.query did not return exactly the captured codex run: $q"
  exit 1
fi
[ "$query_check" = "OK" ] || { echo "FAIL: agent.run.query validation: $q"; exit 1; }

g="$("${RAW[@]}" -cap agent.run.get -kind query -service default-agent-harness -authority agent-runs-main \
  -payload "{\"agent_run_id\":\"$RUN_ID\"}")"
printf '%s\n' "$g"
if ! get_check="$(printf '%s\n' "$g" | python3 -c 'import json,sys; run_id=sys.argv[1]; run=json.load(sys.stdin).get("agent_run", {}); assert run.get("id")==run_id and run.get("provider")=="codex" and run.get("status")=="COMPLETED"; print("OK")' "$RUN_ID")"; then
  echo "FAIL: agent.run.get did not return the completed codex run: $g"
  exit 1
fi
[ "$get_check" = "OK" ] || { echo "FAIL: agent.run.get validation: $g"; exit 1; }

ds="$(git -C "$WSPATH" diff --stat)"
printf '%s\n' "$ds"
[ -n "$ds" ] || { echo "FAIL: codex made no change"; exit 1; }

if ! BLOB="$(printf '%s\n' "$g" | python3 -c 'import json,sys; ref=json.load(sys.stdin).get("agent_run", {}).get("raw_session_ref", ""); assert ref; print(ref)')"; then
  echo "FAIL: no raw_session_ref: $g"
  exit 1
fi
rb="$("${RAW[@]}" \
  -cap blob.get -kind query -service default-blob -authority blob-main \
  -payload "{\"uri\":\"$BLOB\"}")"
if ! blob_check="$(printf '%s\n' "$rb" | python3 -c 'import base64,json,sys; raw=base64.b64decode(json.load(sys.stdin)["content_base64"]).decode("utf-8", "replace"); assert raw and "== result: COMPLETED ==" in raw; print("OK")')"; then
  echo "FAIL: transcript blob is empty or lacks the assembled result line"
  exit 1
fi
[ "$blob_check" = "OK" ] || { echo "FAIL: transcript blob validation"; exit 1; }

echo "REAL PROVIDER (codex) VERIFY: OK"
