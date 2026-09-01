#!/usr/bin/env bash
# Kernel lifecycle for live-integration scripts (smoke, qualification).
# Source this AFTER `cd`-ing to the repo root. It sets DATA/SOCK/tokens, defines
# build_bins + kill_kernel_tree + restart_kernel, exports them, and installs an
# EXIT trap that SIGKILLs the kernel process tree and removes DATA.

DATA="$(mktemp -d)"
SOCK="$DATA/kernel.sock"
export VIBE_DATA_ROOT="$DATA/data"
TOKEN='m1-local-cli-token'
DEV_TOKEN='m1-dev-token'
export VIBE_CLIENT_TOKEN="$TOKEN"
export DEV_TOKEN
KPID=""

build_bins() { bash scripts/build.sh >/dev/null; }

# kill_kernel_tree stops the kernel AND its plugin child processes. The kernel
# spawns plugins via exec.CommandContext; SIGKILL to those children on ctx-cancel
# is async, so without an explicit tree kill each restart leaks ~7 plugin
# processes. Accumulated orphans starve the CPU and make a fresh plugin's 5s
# handshake time out.
kill_kernel_tree() {
  [ -n "${1:-}" ] || return 0
  # Kill the plugin children FIRST, while the kernel is still their parent — once
  # the kernel exits they are reparented to init and `pkill -P` can't find them.
  pkill -KILL -P "$1" 2>/dev/null || true
  for _ in $(seq 1 100); do
    pgrep -P "$1" >/dev/null 2>&1 || break
    sleep 0.02
  done
  kill -KILL "$1" 2>/dev/null || true
  wait "$1" 2>/dev/null || true
}

restart_kernel() {
  kill_kernel_tree "${KPID:-}"
  rm -f "$SOCK"
  .bin/vibe-kernel -plugins ./plugins/manifests -policy ./config/m1-policy.json \
    -bindings ./config/m1-bindings.json -contracts ./contracts -socket "$SOCK" \
    >>"$DATA/kernel.log" 2>&1 &
  KPID=$!
  for _ in $(seq 1 300); do [ -S "$SOCK" ] && break; sleep 0.03; done
  [ -S "$SOCK" ] || { echo "FAIL: kernel socket did not appear"; cat "$DATA/kernel.log"; exit 1; }
  # The socket binds before routing settles; probe a dependency-free provider
  # (event.journal) and the shared dependency (blob) until both answer.
  for _ in $(seq 1 200); do
    if .bin/vibe-raw -socket "$SOCK" -identity local-cli -token "$TOKEN" \
         -cap event.journal.replay -kind query -service default-event-journal \
         -authority journal-main -payload '{}' >/dev/null 2>&1 \
       && .bin/vibe-raw -socket "$SOCK" -identity local-cli -token "$TOKEN" \
         -cap blob.stat -kind query -service default-blob -authority blob-main \
         -payload '{"uri":"blob://sha256/0000000000000000000000000000000000000000000000000000000000000000"}' >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.05
  done
  echo "FAIL: kernel did not become query-ready after restart"; cat "$DATA/kernel.log"; exit 1
}

export SOCK DATA TOKEN KPID
export -f restart_kernel
trap 'kill_kernel_tree "${KPID:-}"; rm -rf "$DATA"' EXIT
