#!/usr/bin/env python3
"""Run and compare the reproducible Vision -> MLP V1 shadow benchmark."""

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
FORBIDDEN_KEYS = {"bbox", "crop", "embedding", "face_image", "raw_frame"}


def read_json(path: Path) -> dict[str, Any]:
    return json.loads(path.read_text(encoding="utf-8"))


def read_jsonl(path: Path) -> list[dict[str, Any]]:
    return [json.loads(line) for line in path.read_text(encoding="utf-8").splitlines() if line.strip()]


def source_frame_count(path: Path) -> int:
    capture = cv2.VideoCapture(str(path))
    try:
        return int(capture.get(cv2.CAP_PROP_FRAME_COUNT) or 0)
    finally:
        capture.release()


def contains_raw_key(value: Any) -> bool:
    if isinstance(value, dict):
        if any(str(key).lower() in FORBIDDEN_KEYS for key in value):
            return True
        return any(contains_raw_key(item) for item in value.values())
    if isinstance(value, list):
        return any(contains_raw_key(item) for item in value)
    return False


def observation_signature(observations: list[dict[str, Any]]) -> list[Any]:
    result = []
    for item in observations:
        tracks = []
        for track in item.get("tracks", []):
            tracks.append((
                track.get("subject_type"),
                track.get("state"),
                int(track.get("detection_count", 0)),
                round(float(track.get("confidence", 0.0)), 6),
            ))
        result.append((
            item.get("priority_hint"),
            item.get("priority_state"),
            item.get("topology_class"),
            item.get("trigger"),
            tuple(tracks),
            (item.get("backend") or {}).get("status"),
        ))
    return result


def teacher_mlp_signature(lines: list[dict[str, Any]]) -> list[dict[str, Any]]:
    result = []
    for item in lines:
        copy = json.loads(json.dumps(item))
        copy.pop("episode_id", None)
        mlp = copy.get("mlp")
        if isinstance(mlp, dict):
            mlp.pop("manifest_sha256", None)
        result.append(copy)
    return result


def collect_run(root: Path, clip: Path) -> dict[str, Any]:
    segment_root = root / "segment-replay"
    segment_summary = read_json(segment_root / "summary.json")
    contract = read_json(segment_root / "summary.contract.json")
    manifest = read_json(segment_root / "manifest.json")
    observations = read_jsonl(segment_root / "observations.jsonl")
    mlp_summary = read_json(root / "summary.json")
    teacher_mlp = read_jsonl(root / "teacher_vs_mlp.jsonl")
    metrics = contract.get("metrics", {})
    backend = contract.get("backend", {})
    process_metrics = read_json(root / "process_metrics.json")
    source_frames = source_frame_count(clip)
    track_ids = {
        track.get("track_id")
        for item in observations
        for track in item.get("tracks", [])
        if track.get("track_id")
    }
    raw_outputs = observations + [contract]
    return {
        "root": str(root),
        "first_observation_latency_ms": metrics.get("first_observation_wall_ms", 0.0),
        "candidate_latency_ms": segment_summary["priority_metrics"]["trigger_to_candidate_ms"],
        "confirmed_latency_ms": segment_summary["priority_metrics"]["trigger_to_confirmed_ms"],
        "vision_wall_ms": metrics.get("vision_wall_latency_ms", 0.0),
        "detector_compute_cumulative_ms": backend.get("detector_compute_sum_ms", metrics.get("detector_compute_sum_ms", 0.0)),
        "frames_read": source_frames,
        "frames_sampled": backend.get("frames_sampled", metrics.get("frames_sampled", 0)),
        "frames_ignored": max(0, source_frames - int(backend.get("frames_sampled", metrics.get("frames_sampled", 0)))),
        "max_frames_in_flight": metrics.get("peak_frames_in_flight", 0),
        "rss_mb": round(float(process_metrics.get("max_rss_kb_children", 0)) / 1024.0, 3),
        "mlp_head_latency_ms": {head: mlp_summary.get("latency_ms_by_head", {}).get(head, 0.0) for head in HEADS},
        "observations": segment_summary.get("observations", len(observations)),
        "segments": segment_summary.get("segments", len(manifest.get("segments", []))),
        "tracks": len(track_ids),
        "summaries": segment_summary.get("final_summary_count", 0),
        "advisory_shadow": mlp_summary.get("cognitive_mode") == "advisory_shadow",
        "physical_action_executed": mlp_summary.get("physical_action_executed") is True,
        "backend_status": backend.get("status"),
        "functional_signature": {
            "observations": observation_signature(observations),
            "priorities": [(item.get("priority_hint"), item.get("priority_state")) for item in observations],
            "teacher_mlp": teacher_mlp_signature(teacher_mlp),
            "summary_count": segment_summary.get("final_summary_count", 0),
            "tracks": len(track_ids),
            "advisory_shadow": mlp_summary.get("cognitive_mode") == "advisory_shadow",
            "physical_action_executed": mlp_summary.get("physical_action_executed") is True,
            "raw_vision_absent": not any(contains_raw_key(item) for item in raw_outputs),
        },
        "process_wall_ms": process_metrics.get("wall_ms", 0.0),
        "process_exit_code": process_metrics.get("exit_code"),
    }


def run_after(args: argparse.Namespace) -> None:
    after = Path(args.after)
    if after.exists():
        raise SystemExit(f"after output already exists: {after}")
    after.mkdir(parents=True)
    env = os.environ.copy()
    env.update({
        "COGNITIVE_PYTHON": args.cognitive_python,
        "VISION_PYTHON": args.vision_python,
        "COGNITIVE_BUNDLE": args.cognitive_bundle,
        "CLIP": str(Path(args.clip).resolve()),
        "OUT": str(after.resolve()),
    })
    started = time.perf_counter()
    result = subprocess.run(["make", "e2e-vision-mlp-v1"], cwd=args.repo, env=env, text=True, capture_output=True)
    process = {
        "exit_code": result.returncode,
        "wall_ms": round((time.perf_counter() - started) * 1000.0, 3),
        "max_rss_kb_children": resource.getrusage(resource.RUSAGE_CHILDREN).ru_maxrss,
    }
    (after / "make.stdout.log").write_text(result.stdout, encoding="utf-8")
    (after / "make.stderr.log").write_text(result.stderr, encoding="utf-8")
    (after / "process_metrics.json").write_text(json.dumps(process, indent=2) + "\n", encoding="utf-8")
    if result.returncode != 0:
        sys.stderr.write(result.stdout[-4000:])
        sys.stderr.write(result.stderr[-4000:])
        raise SystemExit(result.returncode)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", type=Path, required=True)
    parser.add_argument("--clip", type=Path, required=True)
    parser.add_argument("--before", type=Path, required=True)
    parser.add_argument("--after", type=Path, required=True)
    parser.add_argument("--cognitive-python", required=True)
    parser.add_argument("--vision-python", required=True)
    parser.add_argument("--cognitive-bundle", required=True)
    args = parser.parse_args()
    if not args.clip.is_file():
        raise SystemExit(f"clip is not a regular file: {args.clip}")
    if not args.before.is_dir():
        raise SystemExit(f"before output is missing: {args.before}")
    run_after(args)
    before = collect_run(args.before, args.clip)
    after = collect_run(args.after, args.clip)
    numeric = (
        "first_observation_latency_ms", "candidate_latency_ms", "confirmed_latency_ms",
        "vision_wall_ms", "detector_compute_cumulative_ms", "frames_read", "frames_sampled",
        "frames_ignored", "max_frames_in_flight", "rss_mb", "observations", "segments",
        "tracks", "summaries", "process_wall_ms",
    )
    delta = {key: round(float(after[key]) - float(before[key]), 3) for key in numeric}
    for head in HEADS:
        delta[f"mlp_{head}_latency_ms"] = round(
            float(after["mlp_head_latency_ms"][head]) - float(before["mlp_head_latency_ms"][head]), 3
        )
    functional_equal = before["functional_signature"] == after["functional_signature"]
    report = {
        "schema": "synora.perf-baseline-v1",
        "clip": str(args.clip.resolve()),
        "before": before,
        "after": after,
        "delta_after_minus_before": delta,
        "functional_output_identical": functional_equal,
        "criteria": {
            "same_episode_transitions": before["functional_signature"]["priorities"] == after["functional_signature"]["priorities"],
            "same_useful_observations": before["functional_signature"]["observations"] == after["functional_signature"]["observations"],
            "one_final_summary": before["summaries"] == after["summaries"] == 1,
            "same_teacher_and_mlp_shadow": before["functional_signature"]["teacher_mlp"] == after["functional_signature"]["teacher_mlp"],
            "no_raw_vision_on_bus": before["functional_signature"]["raw_vision_absent"] and after["functional_signature"]["raw_vision_absent"],
            "no_physical_action": not before["physical_action_executed"] and not after["physical_action_executed"],
            "max_three_frames_in_flight": after["max_frames_in_flight"] <= 3,
        },
    }
    report_path = args.after / "perf-baseline.json"
    report_path.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps({"report": str(report_path), "functional_output_identical": functional_equal, "delta": delta}, sort_keys=True))
    if not functional_equal or not all(report["criteria"].values()):
        print(json.dumps(report["criteria"], sort_keys=True), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
