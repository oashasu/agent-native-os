#!/usr/bin/env bash
# M1 smoke: real kernel + foundation/domain plugins, including restart survival.
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/lib/kernel-harness.sh
build_bins

restart_kernel

append_out="$(.bin/vibe-raw -socket "$SOCK" -identity m1-dev -token "$DEV_TOKEN" \
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

.bin/vibe -socket "$SOCK" -identity m1-dev -token "$DEV_TOKEN" \
  task transition "$WC_ID" -to IN_PROGRESS -expected-version 1 | grep -q 'IN_PROGRESS' \
  || { echo "FAIL: transition"; exit 1; }

blob_out="$(.bin/vibe-raw -socket "$SOCK" -identity m1-dev -token "$DEV_TOKEN" \
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
source scripts/smoke-artifact.sh
source scripts/smoke-review-session.sh
source scripts/smoke-workflow.sh

echo "M1 SMOKE: PASSED"
