#!/usr/bin/env bash
# Build + launch the M1 kernel in the foreground.
set -euo pipefail
cd "$(dirname "$0")/.."
bash scripts/build.sh
export VIBE_DATA_ROOT="${VIBE_DATA_ROOT:-$PWD/.data}"
rm -rf "$VIBE_DATA_ROOT"
exec .bin/vibe-kernel \
  -plugins ./plugins/manifests \
  -policy ./config/m1-policy.json \
  -bindings ./config/m1-bindings.json \
  -contracts ./contracts \
  -socket /tmp/agent-native-os-m1.sock
