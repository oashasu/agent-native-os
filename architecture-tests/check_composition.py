#!/usr/bin/env python3
"""Composition fitness: capability fan-in limits + composition plugins own no state.

check_boundaries.py (in kernel/) guards kernel purity; this guards the plugin
layer against re-growing a modular monolith one level up (review finding A1).
"""
import argparse
import json
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def parse_args():
    p = argparse.ArgumentParser()
    p.add_argument("--root", type=Path, default=ROOT, help="repository root containing architecture-tests/thresholds.json")
    p.add_argument("--manifests", type=Path, default=ROOT / "plugins" / "manifests")
    return p.parse_args()


def main():
    args = parse_args()
    thresholds = json.loads((args.root / "architecture-tests" / "thresholds.json").read_text())
    manifests = sorted(args.manifests.glob("*.manifest.json"))

    errors: list[str] = []
    warnings: list[str] = []
    infos: list[str] = []

    for mp in manifests:
        m = json.loads(mp.read_text())
        pid = m.get("plugin", {}).get("id", mp.name)
        consumes = m.get("consumes", {})
        n = len(consumes.get("required", [])) + len(consumes.get("optional", []))
        is_comp = m.get("composition") is True

        if is_comp:
            infos.append(f"{pid} is a composition plugin, consumes {n} capabilities")
            for ex in m.get("exports", []):
                if ex.get("mode") == "stateful":
                    errors.append(
                        f"{pid}: composition plugin must not own a stateful export ({ex.get('capability')})"
                    )
        elif n >= thresholds["consume_fail"]:
            errors.append(
                f"{pid}: consumes {n} capabilities (>= fail threshold {thresholds['consume_fail']})"
            )
        elif n >= thresholds["consume_warn"]:
            warnings.append(
                f"{pid}: consumes {n} capabilities (>= warn {thresholds['consume_warn']}); mark \"composition\": true if intentional"
            )

    for info in infos:
        print("info:", info)
    for warning in warnings:
        print("warn:", warning)
    if errors:
        print("COMPOSITION FITNESS: FAILED")
        for error in errors:
            print(" -", error)
        return 1
    print(f"COMPOSITION FITNESS: PASSED ({len(manifests)} manifests)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
