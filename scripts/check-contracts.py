#!/usr/bin/env python3
"""Validate a contract catalog: identity match, major/version match, Draft 2020-12 schemas."""
import argparse, json, sys
from pathlib import Path
from jsonschema import Draft202012Validator

def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--root", default="contracts", help="directory containing catalog.json")
    args = ap.parse_args()
    root = Path(args.root).resolve()
    catalog = json.loads((root / "catalog.json").read_text())
    errors: list[str] = []
    for ident, rel in catalog.items():
        p = root / rel
        try:
            d = json.loads(p.read_text())
        except Exception as e:
            errors.append(f"{ident}: cannot read {rel}: {e}")
            continue
        if d.get("contract") != ident:
            errors.append(f"{ident}: schema 'contract' field is {d.get('contract')!r}")
        major = ident.rsplit("@", 1)[1]
        if not str(d.get("version", "")).startswith(major + "."):
            errors.append(f"{ident}: version {d.get('version')!r} does not match major {major}")
        if d.get("kind") not in ("command", "query", "event"):
            errors.append(f"{ident}: kind must be command|query|event, got {d.get('kind')!r}")
        for side in ("request", "response"):
            if d.get("kind") == "event" and side == "response":
                continue
            if side not in d:
                errors.append(f"{ident}: missing '{side}' schema")
                continue
            try:
                Draft202012Validator.check_schema(d[side])
            except Exception as e:
                errors.append(f"{ident} {side}: invalid JSON Schema: {e}")
    if errors:
        print("CONTRACT CHECK: FAILED")
        for e in errors:
            print(" -", e)
        return 1
    print(f"CONTRACT CHECK: PASSED ({len(catalog)} contracts, root={root})")
    return 0

if __name__ == "__main__":
    sys.exit(main())
