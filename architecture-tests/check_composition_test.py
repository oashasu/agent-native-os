#!/usr/bin/env python3
import json
import pathlib
import subprocess
import sys
import tempfile

ROOT = pathlib.Path(__file__).resolve().parents[1]
CHECKER = ROOT / "architecture-tests" / "check_composition.py"


def run(manifests_dir):
    return subprocess.run(
        [sys.executable, str(CHECKER), "--manifests", str(manifests_dir)],
        capture_output=True,
        text=True,
    )


def write(d, name, obj):
    (pathlib.Path(d) / name).write_text(json.dumps(obj))


def base_manifest(plugin_id="org.vibe.wf", *, composition=True, mode="stateless", consumes=18):
    return {
        "manifest_version": 1,
        "plugin": {"id": plugin_id, "version": "1"},
        "runtime": {"protocol": "vibe-plugin/1", "executable": "../bin/wf"},
        "composition": composition,
        "exports": [
            {"capability": "wf.run", "major": 1, "contract": "wf.run@1", "mode": mode}
        ],
        "consumes": {
            "required": [
                {"capability": f"c{i}", "major": 1, "contract": f"c{i}@1"}
                for i in range(consumes)
            ]
        },
    }


def test_composition_plugin_allowed_high_fanin():
    with tempfile.TemporaryDirectory() as d:
        write(d, "wf.manifest.json", base_manifest())
        r = run(d)
        assert r.returncode == 0, r.stdout + r.stderr
        assert "info: org.vibe.wf is a composition plugin, consumes 18 capabilities" in r.stdout


def test_composition_plugin_rejected_when_it_owns_state():
    with tempfile.TemporaryDirectory() as d:
        write(d, "bad.manifest.json", base_manifest(plugin_id="org.vibe.bad", mode="stateful", consumes=1))
        r = run(d)
        assert r.returncode != 0, r.stdout + r.stderr
        assert "composition plugin must not own a stateful export" in r.stdout


def test_non_composition_high_fanin_still_rejected():
    with tempfile.TemporaryDirectory() as d:
        m = base_manifest(plugin_id="org.vibe.monolith", composition=False)
        write(d, "bad.manifest.json", m)
        r = run(d)
        assert r.returncode != 0, r.stdout + r.stderr
        assert "fail threshold" in r.stdout


def main():
    tests = [
        test_composition_plugin_allowed_high_fanin,
        test_composition_plugin_rejected_when_it_owns_state,
        test_non_composition_high_fanin_still_rejected,
    ]
    failed = 0
    for test in tests:
        try:
            test()
            print(f"ok: {test.__name__}")
        except Exception as exc:
            failed += 1
            print(f"FAIL: {test.__name__}: {exc}")
    if failed:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
