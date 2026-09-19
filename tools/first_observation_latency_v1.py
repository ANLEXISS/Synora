#!/usr/bin/env python3
"""Measure first-observation stages over repeated real RKNN replays.

This is read-only measurement.  It does not alter sampling, tracking,
contracts, bundle contents or action behavior.
"""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import statistics
import subprocess
import tempfile
import time


def read(path: Path) -> dict:
    return json.loads(path.read_text(encoding="utf-8"))


def run(repo: Path, clip: Path, python: str, bundle: Path, index: int) -> dict:
    output = Path(tempfile.mkdtemp(prefix=f"synora-first-observation-{index}-"))
    env = {"CLIP": str(clip.resolve()), "OUT": str(output), "PYTHON": python, "COGNITIVE_V1_BUNDLE": str(bundle.resolve())}
    started = time.perf_counter()
    result = subprocess.run(["make", "replay-vision-core-v1"], cwd=repo, env={**os.environ, **env}, text=True, capture_output=True)
    if result.returncode != 0:
        raise SystemExit(result.stderr[-4000:] or result.stdout[-4000:])
    segment = read(output / "segments/vision-real.json")
    observation = read(output / "segments/core-replay.json")
    vision_metrics = segment.get("latencies") or {}
    observations = observation.get("observations", [])
    segments = observation.get("segments", [])
    return {"run": index, "wall_ms": round((time.perf_counter() - started) * 1000.0, 3), "first_observation_ms": vision_metrics.get("first_observation_wall_ms", 0.0), "capture_open_ms": vision_metrics.get("capture_open_wall_ms", 0.0), "first_frame_decode_ms": vision_metrics.get("first_frame_decode_wall_ms", 0.0), "first_detector_batch_ms": vision_metrics.get("first_detector_batch_wall_ms", 0.0), "first_tracking_ms": vision_metrics.get("first_tracking_wall_ms", 0.0), "detector_init_ms": segment.get("detector_init_wall_ms", 0.0), "vision_wall_ms": vision_metrics.get("vision_wall_latency_ms", segment.get("wall_ms", 0.0)), "observations": len(observations) if isinstance(observations, list) else observations, "segments": len(segments) if isinstance(segments, list) else segments, "physical_action_executed": observation.get("physical_action_executed", False)}


def summary(rows: list[dict]) -> dict:
    fields = ("first_observation_ms", "capture_open_ms", "first_frame_decode_ms", "first_detector_batch_ms", "first_tracking_ms", "detector_init_ms", "vision_wall_ms", "wall_ms")
    return {field: {"median": round(statistics.median(row[field] for row in rows), 3), "min": round(min(row[field] for row in rows), 3), "max": round(max(row[field] for row in rows), 3)} for field in fields}


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", type=Path, default=Path("."))
    parser.add_argument("--clip", type=Path, default=Path("/home/rock/test3.mp4"))
    parser.add_argument("--bundle", type=Path, default=Path("build/cognitive-mlp-v1-incident-v2"))
    parser.add_argument("--python", default="python3")
    parser.add_argument("--runs", type=int, default=5)
    parser.add_argument("--out", type=Path, default=Path("build/first-observation-latency-v1.json"))
    args = parser.parse_args()
    rows = [run(args.repo.resolve(), args.clip.resolve(), args.python, args.bundle.resolve(), index) for index in range(args.runs)]
    report = {"schema_version": "synora.first-observation-latency-v1", "clip": str(args.clip.resolve()), "runs": rows, "summary": summary(rows), "functional_invariants": {"same_observations": len({row["observations"] for row in rows}) == 1, "same_segments": len({row["segments"] for row in rows}) == 1, "physical_action_executed": any(row["physical_action_executed"] for row in rows), "attribution": "first observation is inside Vision process_video; detector initialization is measured separately and does not explain the in-process first-observation clock unless process startup is changed"}}
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps({"report": str(args.out), "summary": report["summary"], "invariants": report["functional_invariants"]}, sort_keys=True))
    return 0 if not report["functional_invariants"]["physical_action_executed"] and report["functional_invariants"]["same_observations"] and report["functional_invariants"]["same_segments"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
