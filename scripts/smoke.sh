#!/usr/bin/env bash
# M1 smoke: kernel comes up with the event-journal foundation plugin and one
# event round-trips through append + replay.
set -euo pipefail
cd "$(dirname "$0")/.."
bash scripts/build.sh >/dev/null

DATA="$(mktemp -d)"
SOCK="$DATA/kernel.sock"
export VIBE_DATA_ROOT="$DATA/data"

.bin/vibe-kernel -plugins ./plugins/manifests -policy ./config/m1-policy.json \
  -bindings ./config/m1-bindings.json -contracts ./contracts -socket "$SOCK" \
  >"$DATA/kernel.log" 2>&1 &
KPID=$!
trap 'kill $KPID 2>/dev/null; wait $KPID 2>/dev/null; rm -rf "$DATA"' EXIT

for _ in $(seq 1 300); do [ -S "$SOCK" ] && break; sleep 0.03; done
[ -S "$SOCK" ] || { echo "FAIL: kernel socket never appeared"; cat "$DATA/kernel.log"; exit 1; }

TOKEN='m1-local-cli-token'
append_out="$(.bin/vibe -socket "$SOCK" -identity local-cli -token "$TOKEN" \
  -cap event.journal.append -kind command \
  -service default-event-journal -authority journal-main \
  -payload '{"type":"m1.smoke","source":"scripts/smoke.sh","payload":{"ok":true}}')"
echo "$append_out" | grep -q '"m1.smoke"' || { echo "FAIL: append response: $append_out"; cat "$DATA/kernel.log"; exit 1; }

replay_out="$(.bin/vibe -socket "$SOCK" -identity local-cli -token "$TOKEN" \
  -cap event.journal.replay -kind query \
  -service default-event-journal -authority journal-main -payload '{}')"
echo "$replay_out" | grep -q '"m1.smoke"' || { echo "FAIL: replay did not contain the appended event: $replay_out"; exit 1; }

echo "M1 SMOKE: PASSED"
