#!/usr/bin/env bash
# Build the kernel binaries and every M1 plugin into .bin/ and plugins/bin/.
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p .bin plugins/bin

( cd kernel && go build -o "$OLDPWD/.bin/vibe-kernel" ./cmd/kernel && go build -o "$OLDPWD/.bin/vibe-raw" ./cmd/vibe )

for d in plugins/foundation/* plugins/*/; do
  [ -f "$d/main.go" ] || continue
  name="$(basename "$d")"
  [ "$name" = "_template" ] && continue
  ( cd "$d" && go build -o "$OLDPWD/plugins/bin/$name" . )
  echo "built plugin: $name"
done

( cd cli/vibe && go build -o "$OLDPWD/.bin/vibe" . )
echo "built cli: vibe"
echo "BUILD OK"
