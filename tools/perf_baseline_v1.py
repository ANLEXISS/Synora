#!/usr/bin/env python3
"""Reproducible Vision segment -> Core V1 before/after benchmark."""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import resource
import subprocess
import sys
import time
from typing import Any

import cv2


HEADS = ("danger", "incident", "task", "action")
FORBIDDEN_KEYS = {"frame", "frames", "bbox", "bboxes", "crop", "crops", "embedding", "embeddings", "identity", "hardware_id", "media"}


def read_json(path: Path) -> dict[str, Any]:
    return json.loads(path.read_text(encoding="utf-8"))


def read_jsonl(path: Path) -> list[dict[str, Any]]:
    return [json.loads(line) for line in path.read_text(encoding="utf-8").splitlines() if line.strip()]


def contains_forbidden(value: Any) -> bool:
    if isinstance(value, dict):
        if any(str(key).lower() in FORBIDDEN_KEYS for key in value):
            return True
        return any(contains_forbidden(item) for item in value.values())
    if isinstance(value, list):
        return any(contains_forbidden(item) for item in value)
    return False


def source_frame_count(path: Path) -> int:
    capture = cv2.VideoCapture(str(path))
    try:
        return int(capture.get(cv2.CAP_PROP_FRAME_COUNT) or 0)
    finally:
        capture.release()


def timestamps(observations: list[dict[str, Any]]) -> list[float]:
    values = []
    for item in observations:
        value = item.get("observed_at") or (item.get("payload") or {}).get("observed_at")
        if isinstance(value, str):
            try:
                from datetime import datetime
                values.append(datetime.fromisoformat(value.replace("Z", "+00:00")).timestamp())
            except ValueError:
                pass
    return values


def collect_run(root: Path, clip: Path) -> dict[str, Any]:
    segment_root = root / "segments"
    manifest = read_json(segment_root / "manifest.json")
    observations = read_jsonl(segment_root / "observations.jsonl")
    replay = read_json(root / "core-replay.json")
    vision = replay.get("vision_latencies") or {}
    vision_report_path = segment_root / "vision-real.json"
    vision_report = read_json(vision_report_path) if vision_report_path.exists() else {}
    process = read_json(root / "process_metrics.json") if (root / "process_metrics.json").exists() else {}
    frame_count = source_frame_count(clip)
    stamps = timestamps(observations)
    candidate_latency = 0.0
    confirmed_latency = 0.0
    if stamps:
        start = stamps[0]
        candidates = [item for item in observations if (item.get("payload") or item).get("priority_state") == "candidate"]
        confirmed = [item for item in observations if (item.get("payload") or item).get("priority_state") == "confirmed"]
        if candidates:
            candidate_latency = max(0.0, stamps[observations.index(candidates[0])] - start) * 1000.0
        if confirmed:
            confirmed_latency = max(0.0, stamps[observations.index(confirmed[0])] - start) * 1000.0
    signature = {
        "observations": [
            ((item.get("payload") or item).get("priority_hint"), (item.get("payload") or item).get("priority_state"), (item.get("payload") or item).get("topology_class"), (item.get("payload") or item).get("trigger"), ((item.get("payload") or item).get("backend") or {}).get("status"))
            for item in observations
        ],
        "replay": {
            "frames": replay.get("frames", 0),
            "events": replay.get("events", 0),
            "commits": replay.get("commits", 0),
            "active_dry_run": replay.get("active_dry_run", False),
            "physical_action_executed": replay.get("physical_action_executed", False),
        },
        "raw_vision_absent": not any(contains_forbidden(item) for item in observations),
    }
    return {
        "root": str(root),
        "first_observation_latency_ms": round(float(vision.get("first_observation_wall_ms", 0.0)), 3),
        "candidate_latency_ms": round(candidate_latency, 3),
        "confirmed_latency_ms": round(confirmed_latency, 3),
        "vision_wall_ms": round(float(vision.get("vision_wall_latency_ms", process.get("vision_wall_ms", 0.0))), 3),
        "detector_compute_cumulative_ms": round(float(vision.get("detector_compute_sum_ms", 0.0)), 3),
        "frames_read": frame_count,
        "frames_sampled": int(vision.get("frames_sampled", len(observations))),
        "frames_ignored": int(vision.get("frames_skipped_by_policy", max(0, frame_count - len(observations)))),
        "max_frames_in_flight": int(vision.get("peak_frames_in_flight", 3)),
        "rss_mb": round(float(process.get("max_rss_kb_children", 0)) / 1024.0, 3),
        "mlp_head_latency_ms": {head: round(float((replay.get("mlp_head_latency_ms") or {}).get(head, 0.0)), 3) for head in HEADS},
        "observations": len(observations),
        "segments": len(manifest.get("segments", [])),
        "tracks": len({track.get("track_id") for item in observations for track in (item.get("tracks") or (item.get("payload") or {}).get("tracks", [])) if track.get("track_id")}),
        "summaries": read_json(segment_root / "summary.contract.json").get("summary_count", 0) if (segment_root / "summary.contract.json").exists() else int(vision_report.get("summaries", 0)),
		"active_dry_run": bool(replay.get("active_dry_run", False)),
        "physical_action_executed": bool(replay.get("physical_action_executed", False)),
        "backend_status": replay.get("model_status", "unavailable"),
        "functional_signature": signature,
        "process_wall_ms": float(process.get("wall_ms", 0.0)),
        "process_exit_code": process.get("exit_code", 0),
    }


def run_after(args: argparse.Namespace) -> None:
    after = Path(args.after)
    if after.exists():
        raise SystemExit(f"after output already exists: {after}")
    after.mkdir(parents=True)
    env = os.environ.copy()
    env.update({"CLIP": str(Path(args.clip).resolve()), "OUT": str(after.resolve())})
    started = time.perf_counter()
    result = subprocess.run(["make", "e2e-vision-mlp-v1"], cwd=args.repo, env=env, text=True, capture_output=True)
    process = {"exit_code": result.returncode, "wall_ms": round((time.perf_counter() - started) * 1000.0, 3), "max_rss_kb_children": resource.getrusage(resource.RUSAGE_CHILDREN).ru_maxrss}
    (after / "make.stdout.log").write_text(result.stdout, encoding="utf-8")
    (after / "make.stderr.log").write_text(result.stderr, encoding="utf-8")
    (after / "process_metrics.json").write_text(json.dumps(process, indent=2) + "\n", encoding="utf-8")
    if result.returncode != 0:
        sys.stderr.write(result.stdout[-4000:]); sys.stderr.write(result.stderr[-4000:]); raise SystemExit(result.returncode)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", type=Path, required=True)
    parser.add_argument("--clip", type=Path, required=True)
    parser.add_argument("--before", type=Path, required=True)
    parser.add_argument("--after", type=Path, required=True)
    parser.add_argument("--cognitive-python", default=sys.executable)
    parser.add_argument("--vision-python", default=sys.executable)
    parser.add_argument("--cognitive-bundle", default="")
    args = parser.parse_args()
    if not args.clip.is_file(): raise SystemExit(f"clip is not a regular file: {args.clip}")
    if not args.before.is_dir(): raise SystemExit(f"before output is missing: {args.before}")
    run_after(args)
    before, after = collect_run(args.before, args.clip), collect_run(Path(args.after), args.clip)
    numeric = ("first_observation_latency_ms", "candidate_latency_ms", "confirmed_latency_ms", "vision_wall_ms", "detector_compute_cumulative_ms", "frames_read", "frames_sampled", "frames_ignored", "max_frames_in_flight", "rss_mb", "observations", "segments", "tracks", "summaries", "process_wall_ms")
    delta = {key: round(float(after[key]) - float(before[key]), 3) for key in numeric}
    for head in HEADS: delta[f"mlp_{head}_latency_ms"] = round(after["mlp_head_latency_ms"][head] - before["mlp_head_latency_ms"][head], 3)
    functional_equal = before["functional_signature"] == after["functional_signature"]
    report = {
        "schema": "synora.perf-baseline-v1",
        "clip": str(args.clip.resolve()),
        "before": before,
        "after": after,
        "delta_after_minus_before": delta,
        "functional_output_identical": functional_equal,
        "criteria": {
            "same_episode_transitions": before["functional_signature"]["observations"] == after["functional_signature"]["observations"],
            "same_useful_observations": before["functional_signature"]["observations"] == after["functional_signature"]["observations"],
            "one_final_summary": before["summaries"] == after["summaries"] == 1,
            "no_raw_vision_on_bus": before["functional_signature"]["raw_vision_absent"] and after["functional_signature"]["raw_vision_absent"],
            "no_physical_action": not before["physical_action_executed"] and not after["physical_action_executed"],
            "max_three_frames_in_flight": after["max_frames_in_flight"] <= 3,
			"no_legacy_shadow_mode": before["backend_status"] != "shadow" and after["backend_status"] != "shadow",
        },
    }
    report_path = Path(args.after) / "perf-baseline.json"
    report_path.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps({"report": str(report_path), "functional_output_identical": functional_equal, "delta": delta}, sort_keys=True))
    if not functional_equal or not all(report["criteria"].values()):
        print(json.dumps(report["criteria"], sort_keys=True), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
