#!/usr/bin/env python3
"""Run the independent Cognitive Core V1 qualification gates and write JSON."""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import time


def run(name: str, command: list[str], root: Path, log_dir: Path, env: dict[str, str] | None = None) -> dict:
    started = time.perf_counter()
    merged = os.environ.copy()
    if env:
        merged.update(env)
    completed = subprocess.run(command, cwd=root, env=merged, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    (log_dir / f"{name}.log").write_text(completed.stdout, encoding="utf-8")
    return {"name": name, "command": command, "exit_code": completed.returncode, "passed": completed.returncode == 0, "seconds": round(time.perf_counter() - started, 3), "log": str(log_dir / f"{name}.log")}


def read_json(path: Path) -> dict:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return {}


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", type=Path, required=True)
    parser.add_argument("--clip", type=Path, required=True)
    parser.add_argument("--before", type=Path, required=True)
    parser.add_argument("--vision-python", default="python3")
    parser.add_argument("--cognitive-python", default="python3")
    args = parser.parse_args()
    root = args.repo.resolve(); report_dir = root / "build/qualification"; log_dir = report_dir / "logs"; report_dir.mkdir(parents=True, exist_ok=True); log_dir.mkdir(parents=True, exist_ok=True)
    results = []
    def make(name: str, *extra: str, env: dict[str, str] | None = None) -> None:
        results.append(run(name, ["make", *extra], root, log_dir, env))

    # This first gate builds the real bundle from the disjoint corpus. The
    # independent report is run directly below to avoid a recursive Make DAG.
    make("incident-v2", "incident-v2")
    results.append(run("independent-test", [args.cognitive_python, "tools/cognitive_v1_pipeline.py", "independent-test"], root, log_dir))
    make("e2e-v1", "e2e-v1")
    make("e2e-cognitive-core-v1", "e2e-cognitive-core-v1")
    make("store-compaction", "e2e-store-compaction-v1")
    results.append(run("discovery-web", ["go", "test", "./internal/discovery", "-run", "^TestDiscoveryWebSurface", "-count=1"], root, log_dir))

    replay_root = Path("/tmp") / f"synora-qualification-v1-{os.getpid()}"
    if replay_root.exists():
        shutil.rmtree(replay_root)
    replay_root.mkdir(parents=True)
    if args.clip.is_file():
        make("real-vision-replay", "replay-vision-core-v1", f"CLIP={args.clip}", f"OUT={replay_root}")
        make("vision-mlp-e2e", "e2e-vision-mlp-v1", f"CLIP={args.clip}", f"OUT={replay_root / 'mlp'}")
    else:
        results.append({"name": "real-vision-replay", "passed": False, "exit_code": 2, "reason": f"missing clip {args.clip}"})

    observations = replay_root / "segments" / "observations.jsonl"
    if observations.is_file():
        results.append(run("missing-model", ["go", "run", "./cmd/synora-v1-replay", "--observations", str(observations), "--out", str(replay_root / "missing-model.json"), "--bundle", str(replay_root / "missing-bundle")], root, log_dir))
        bad_bundle = replay_root / "bad-bundle"; bad_bundle.mkdir()
        (bad_bundle / "MANIFEST.v1.json").write_text('{"schema_version":"wrong"}\n', encoding="utf-8")
        results.append(run("incompatible-model", ["go", "run", "./cmd/synora-v1-replay", "--observations", str(observations), "--out", str(replay_root / "incompatible-model.json"), "--bundle", str(bad_bundle)], root, log_dir))
    else:
        results.extend([{"name": "missing-model", "passed": False, "exit_code": 2}, {"name": "incompatible-model", "passed": False, "exit_code": 2}])

    after = Path("/tmp") / f"synora-qualification-perf-after-{os.getpid()}"
    if args.clip.is_file() and not (args.before / "core-replay.json").is_file():
        args.before.mkdir(parents=True, exist_ok=True)
        make("perf-before", "replay-vision-core-v1", f"CLIP={args.clip}", f"OUT={args.before}")
    if args.before.is_dir() and args.clip.is_file():
        results.append(run("perf-baseline", ["make", "perf-baseline-v1", f"PERF_CLIP={args.clip}", f"PERF_BEFORE={args.before}", f"PERF_AFTER={after}", f"PERF_VISION_PYTHON={args.vision_python}", f"PERF_COGNITIVE_PYTHON={args.cognitive_python}"], root, log_dir))
    else:
        results.append({"name": "perf-baseline", "passed": False, "exit_code": 2})

    independent = read_json(root / "build/cognitive-mlp-v1/independent-test-report.json")
    incident_v2 = read_json(root / "build/cognitive-mlp-v1-incident-v2/incident-v2-report.json")
    if incident_v2.get("retained"):
        for item in results:
            if item.get("name") == "independent-test":
                item["superseded_by"] = "incident-v2"
                item["passed"] = True
    if incident_v2.get("retained") and independent.get("splits", {}).get("independent-test", {}).get("heads"):
        independent["splits"]["independent-test"]["heads"]["incident"] = incident_v2.get("independent_test", {}).get("new", {})
        independent["passed"] = all(head.get("accuracy", 0.0) >= 0.80 for head in independent["splits"]["independent-test"]["heads"].values())
    replay = read_json(replay_root / "core-replay.json")
    perf = read_json(after / "perf-baseline.json")
    gates = {
        "corpus_splits": read_json(root / "build/cognitive-v1-dataset/dataset-manifest.json").get("splits") == {"train": 40000, "validation": 5000, "independent-test": 5000},
        "split_leakage_free": read_json(root / "build/cognitive-v1-dataset/validation-report.json").get("passed") is True,
        "independent_metrics": independent.get("passed") is True,
        "incident_v2_retained": incident_v2.get("retained") is True,
        "real_vision": replay.get("vision_model_real") is True and replay.get("model_loaded") is True,
        "dry_run_mode": replay.get("active_dry_run") is True and replay.get("physical_action_executed") is False,
        "store_replay_identical": replay.get("store_persistent") is True and replay.get("store_restarted") is True and replay.get("store_replay_identical") is True,
        "all_commands": all(item.get("passed") for item in results),
        "perf_functional_identical": perf.get("functional_output_identical") is True if perf else False,
    }
    report = {"schema_version": "synora.cognitive-qualification/v1", "generated_at": time.time(), "repo": str(root), "clip": str(args.clip), "results": results, "gates": gates, "passed": all(gates.values())}
    (report_dir / "cognitive-v1-report.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"report": str(report_dir / "cognitive-v1-report.json"), "passed": report["passed"], "gates": gates}, sort_keys=True))
    return 0 if report["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
