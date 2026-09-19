#!/usr/bin/env python3
"""Run the real RKNN human detector over a clip and emit safe Vision events."""

from __future__ import annotations

import argparse
from datetime import datetime, timedelta, timezone
import json
import os
from pathlib import Path
import subprocess
import sys
import time


def fail(output: Path, message: str, *, model_path: str = "") -> int:
    output.mkdir(parents=True, exist_ok=True)
    report = {
        "schema_version": "synora.vision.real-replay/v1",
        "vision_model_real": False,
        "model_loaded": False,
        "model_path": model_path,
        "status": "unavailable",
        "error": message,
    }
    (output / "vision-real.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(report, sort_keys=True), file=sys.stderr)
    return 3


def parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--clip", required=True)
    p.add_argument("--manifest", required=True)
    p.add_argument("--out", required=True)
    p.add_argument("--camera-id", default="cam_entry_01")
    p.add_argument("--node-id", default="entry")
    p.add_argument("--zone", default="protected_interior")
    p.add_argument("--trigger", default="motion")
    p.add_argument("--model", default=os.getenv("SYNORA_YOLO_MODEL_PATH", "/var/lib/synora/models/yolov8.rknn"))
    return p


def main() -> int:
    args = parser().parse_args()
    clip = Path(args.clip).expanduser().resolve()
    output = Path(args.out).expanduser().resolve()
    model = Path(args.model).expanduser().resolve()
    if not clip.is_file():
        return fail(output, f"clip is not a regular file: {clip}", model_path=str(model))
    if not model.is_file() or model.suffix != ".rknn":
        return fail(output, f"RKNN model is missing or invalid: {model}", model_path=str(model))

    worker_root = Path(__file__).resolve().parent
    sys.path.insert(0, str(worker_root))
    try:
        from modules.detect.person_detector import PersonDetector
        from core.clip_pipeline_v1 import ClipMetadata, Topology, TopologyClass, VisionClipPipelineV1
        from core.detector_backend import ExistingDetectorBackend
    except Exception as exc:
        return fail(output, f"Vision runtime import failed: {exc}", model_path=str(model))

    output.mkdir(parents=True, exist_ok=True)
    os.environ["SYNORA_YOLO_MODEL_PATH"] = str(model)
    started = time.perf_counter()
    detector = PersonDetector(debug_enabled=False)
    if not detector.available:
        return fail(output, f"RKNN detector unavailable: {detector.error or detector.capability_status}", model_path=str(model))
    backend = ExistingDetectorBackend(detector, timeout_seconds=float(os.getenv("SYNORA_VISION_V1_DETECTOR_TIMEOUT", "5")))
    try:
        manifest = json.loads(Path(args.manifest).read_text(encoding="utf-8"))
        segments = manifest.get("segments", [])
        if not segments:
            return fail(output, "segment manifest is empty", model_path=str(model))
        episode_id = segments[0]["segment"]["episode_id"]
        start_at = datetime.fromisoformat(segments[0]["segment"]["started_at"].replace("Z", "+00:00"))
        end_at = datetime.fromisoformat(segments[-1]["segment"]["ended_at"].replace("Z", "+00:00"))
        clip_meta = ClipMetadata(
            clip_id=Path(args.clip).stem,
            episode_id=episode_id,
            camera_id=args.camera_id,
            topology=Topology(args.node_id, args.zone, TopologyClass(args.zone)),
            trigger_reason=args.trigger,
            started_at=start_at,
            ends_at=end_at,
            clip_ref=None,
        )
        pipeline = VisionClipPipelineV1({
            "sampling_initial_fps": 5.0,
            "sampling_active_fps": 5.0,
            "sampling_stable_fps": 2.0,
            "sampling_quiet_fps": 2.0,
            "sampling_minimum_detection_fps": 1.0,
            "max_crops_per_track": 0,
        })
        events = pipeline.process_video(clip_meta, str(clip), backend, sample_period_seconds=0.2)
        observations = [event for event in events if event.get("type") == "synora.vision.clip-observation/v1"]
        summaries = [event for event in events if event.get("type") == "synora.vision.clip-summary/v1"]
        with (output / "observations.jsonl").open("w", encoding="utf-8") as stream:
            for event in observations:
                payload = dict(event.get("payload") or {})
                payload["real_detection"] = True
                payload["replay_simulation"] = False
                payload.pop("media", None)
                payload.pop("identity", None)
                payload.pop("plate", None)
                payload.pop("sensitive_objects", None)
                stream.write(json.dumps({"type": event["type"], "payload": payload}, sort_keys=True) + "\n")
        diagnostic = backend.diagnostic()
        metrics = {}
        for event in summaries:
            metrics.update(event.get("payload", {}).get("metrics", {}))
        report = {
            "schema_version": "synora.vision.real-replay/v1",
            "vision_model_real": True,
            "model_loaded": True,
            "model_path": str(model),
            "model_backend": "rknn",
            "status": "ok",
            "detections": int(diagnostic.get("detections_total", 0)),
            "segments": len(segments),
            "tracks": len({track.get("track_id") for event in observations for track in event.get("payload", {}).get("tracks", [])}),
            "observations": len(observations),
            "summaries": 1 if summaries else 0,
            "track_summaries": len(summaries),
            "backend": diagnostic,
            "latencies": metrics,
            "wall_ms": round((time.perf_counter() - started) * 1000.0, 3),
        }
        (output / "vision-real.json").write_text(json.dumps(report, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        go = os.environ.get("GO", "go")
        replay = subprocess.run([
            go, "run", "./cmd/synora-v1-replay", "--observations", str(output / "observations.jsonl"),
            "--out", str(output / "core-replay.json"), "--vision-report", str(output / "vision-real.json"),
            "--store-dir", str(output / "store"),
        ], cwd=Path(__file__).resolve().parents[2], check=False)
        if replay.returncode != 0:
            return replay.returncode
        print(json.dumps(report, sort_keys=True))
        return 0
    finally:
        backend.close()
        try:
            detector.runner.close()
        except Exception:
            pass


if __name__ == "__main__":
    raise SystemExit(main())
