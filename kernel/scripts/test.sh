#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
./scripts/build.sh
go test ./...
go test -race ./...
python3 tools/generate_contracts.py --check
python3 tools/test_contract_compat.py
python3 tools/contract_check.py
python3 architecture-tests/check_boundaries.py
python3 tests/integration/m05_qualification.py
