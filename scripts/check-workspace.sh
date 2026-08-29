#!/usr/bin/env bash
# Verifies the Go workspace links kernel + plugins and cross-module imports resolve.
set -euo pipefail
cd "$(dirname "$0")/.."
test -f go.work || { echo "FAIL: go.work missing"; exit 1; }
go build github.com/example/agent-native-microkernel/... github.com/example/agent-native-os/plugins/...
echo "WORKSPACE OK"
