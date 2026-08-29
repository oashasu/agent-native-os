#!/usr/bin/env bash
# M1 smoke: real kernel + foundation/domain plugins, including restart survival.
set -euo pipefail
cd "$(dirname "$0")/.."
bash scripts/build.sh >/dev/null

DATA="$(mktemp -d)"
SOCK="$DATA/kernel.sock"
export VIBE_DATA_ROOT="$DATA/data"
TOKEN='m1-local-cli-token'
export VIBE_CLIENT_TOKEN="$TOKEN"
KPID=""

restart_kernel() {
  if [ -n "${KPID:-}" ]; then
    kill "$KPID" 2>/dev/null || true
    wait "$KPID" 2>/dev/null || true
  fi
  rm -f "$SOCK"
  .bin/vibe-kernel -plugins ./plugins/manifests -policy ./config/m1-policy.json \
    -bindings ./config/m1-bindings.json -contracts ./contracts -socket "$SOCK" \
    >>"$DATA/kernel.log" 2>&1 &
  KPID=$!
  for _ in $(seq 1 300); do [ -S "$SOCK" ] && break; sleep 0.03; done
  [ -S "$SOCK" ] || { echo "FAIL: kernel socket did not appear"; cat "$DATA/kernel.log"; exit 1; }
  # The socket binds after the plugin start loop, but routing to a just-registered
  # stateful provider can still race for a beat. Probe a dependency-free stateful
  # query until it succeeds before returning.
  for _ in $(seq 1 200); do
    .bin/vibe-raw -socket "$SOCK" -identity local-cli -token "$TOKEN" \
      -cap event.journal.replay -kind query -service default-event-journal \
      -authority journal-main -payload '{}' >/dev/null 2>&1 && return 0
    sleep 0.05
  done
  echo "FAIL: kernel did not become query-ready after restart"; cat "$DATA/kernel.log"; exit 1
}
export SOCK DATA TOKEN KPID
export -f restart_kernel
trap 'if [ -n "${KPID:-}" ]; then kill "$KPID" 2>/dev/null; wait "$KPID" 2>/dev/null || true; fi; rm -rf "$DATA"' EXIT

restart_kernel

append_out="$(.bin/vibe-raw -socket "$SOCK" -identity local-cli -token "$TOKEN" \
  -cap event.journal.append -kind command \
  -service default-event-journal -authority journal-main \
  -payload '{"type":"m1.smoke","source":"scripts/smoke.sh","payload":{"ok":true}}')"
echo "$append_out" | grep -q '"m1.smoke"' || { echo "FAIL: append response: $append_out"; cat "$DATA/kernel.log"; exit 1; }

replay_out="$(.bin/vibe-raw -socket "$SOCK" -identity local-cli -token "$TOKEN" \
  -cap event.journal.replay -kind query \
  -service default-event-journal -authority journal-main -payload '{}')"
echo "$replay_out" | grep -q '"m1.smoke"' || { echo "FAIL: replay did not contain the appended event: $replay_out"; exit 1; }

# --- M1.1: work-registry + blob + restart survival ---
create_out="$(.bin/vibe -socket "$SOCK" -identity local-cli -token "$TOKEN" \
  task create -title "smoke task" -goal "prove the slice" -repo fixtures/sample-java-project -ac AC1="mvn test PASS")"
echo "$create_out" | grep -q 'status PLANNED' || { echo "FAIL: task create: $create_out"; cat "$DATA/kernel.log"; exit 1; }
TASK_ID="$(echo "$create_out" | sed -n 's/^task \([^ ]*\).*/\1/p')"
WC_ID="$(echo "$create_out" | sed -n 's/.*wc \([^ ]*\).*/\1/p')"

.bin/vibe -socket "$SOCK" -identity local-cli -token "$TOKEN" \
  task transition "$WC_ID" -to IN_PROGRESS -expected-version 1 | grep -q 'IN_PROGRESS' \
  || { echo "FAIL: transition"; exit 1; }

blob_out="$(.bin/vibe-raw -socket "$SOCK" -identity local-cli -token "$TOKEN" \
  -cap blob.put -kind command -service default-blob -authority blob-main \
  -payload "{\"content_base64\":\"$(printf 'diff-bytes' | base64)\"}")"
BLOB_URI="$(echo "$blob_out" | sed -n 's/.*"uri":"\([^"]*\)".*/\1/p')"
[ -n "$BLOB_URI" ] || { echo "FAIL: blob put: $blob_out"; exit 1; }

restart_kernel

show_out="$(.bin/vibe -socket "$SOCK" -identity local-cli -token "$TOKEN" task show "$TASK_ID")"
echo "$show_out" | grep -q 'status.*IN_PROGRESS' || { echo "FAIL: task did not survive restart: $show_out"; cat "$DATA/kernel.log"; exit 1; }

got_blob="$(.bin/vibe-raw -socket "$SOCK" -identity local-cli -token "$TOKEN" \
  -cap blob.get -kind query -service default-blob -authority blob-main -payload "{\"uri\":\"$BLOB_URI\"}")"
echo "$got_blob" | grep -q "$(printf 'diff-bytes' | base64)" || { echo "FAIL: blob did not survive restart: $got_blob"; exit 1; }

source scripts/smoke-workspace.sh
source scripts/smoke-agent.sh

echo "M1 SMOKE: PASSED"
