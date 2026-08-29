#!/usr/bin/env python3
"""Composition fitness: capability fan-in limits + composition plugins own no state.

check_boundaries.py (in kernel/) guards kernel purity; this guards the plugin
layer against re-growing a modular monolith one level up (review finding A1).
"""
import json, sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
MANIFESTS = sorted((ROOT / "plugins" / "manifests").glob("*.manifest.json"))
TH = json.loads((ROOT / "architecture-tests" / "thresholds.json").read_text())

errors: list[str] = []
warnings: list[str] = []

for mp in MANIFESTS:
    m = json.loads(mp.read_text())
    pid = m.get("plugin", {}).get("id", mp.name)
    consumes = m.get("consumes", {})
    n = len(consumes.get("required", [])) + len(consumes.get("optional", []))
    is_comp = m.get("composition") is True

    if n >= TH["consume_fail"]:
        errors.append(f"{pid}: consumes {n} capabilities (>= fail threshold {TH['consume_fail']})")
    elif n >= TH["consume_warn"] and not is_comp:
        warnings.append(f"{pid}: consumes {n} capabilities (>= warn {TH['consume_warn']}); mark \"composition\": true if intentional")

    if is_comp:
        for ex in m.get("exports", []):
            if ex.get("mode") == "stateful":
                errors.append(f"{pid}: composition plugin must not own a stateful export ({ex.get('capability')})")

for w in warnings:
    print("warn:", w)
if errors:
    print("COMPOSITION FITNESS: FAILED")
    for e in errors:
        print(" -", e)
    sys.exit(1)
print(f"COMPOSITION FITNESS: PASSED ({len(MANIFESTS)} manifests)")
