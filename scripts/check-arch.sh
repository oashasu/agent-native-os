#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
python3 scripts/check-contracts.py --root contracts
python3 architecture-tests/check_composition.py
( cd kernel && python3 architecture-tests/check_boundaries.py )
echo "ARCH CHECKS OK"
