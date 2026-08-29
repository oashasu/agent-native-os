#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
mkdir -p "$ROOT/bin" "$ROOT/plugins/bin"
cd "$ROOT"
go build -o bin/vibe-kernel ./cmd/kernel
go build -o bin/vibe ./cmd/vibe
go build -o plugins/bin/work-registry ./plugins/work-registry
go build -o plugins/bin/work-registry-replica ./plugins/work-registry
go build -o plugins/bin/event-journal ./plugins/event-journal
go build -o plugins/bin/mock-agent ./examples/mock-agent
go build -o plugins/bin/stream-probe ./plugins/stream-probe
go build -o plugins/bin/cancel-probe ./plugins/cancel-probe
go build -o plugins/bin/fence-probe-primary ./plugins/fence-probe
go build -o plugins/bin/fence-probe-replica ./plugins/fence-probe
go build -o plugins/bin/workflow-demo ./plugins/workflow-demo
go build -o plugins/bin/event-probe-publisher ./plugins/event-probe-publisher
go build -o plugins/bin/event-probe-malicious ./plugins/event-probe-malicious
go build -o plugins/bin/event-probe-subscriber-allowed ./plugins/event-probe-subscriber
go build -o plugins/bin/event-probe-subscriber-denied ./plugins/event-probe-subscriber
