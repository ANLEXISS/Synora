#!/usr/bin/env python3
"""Probe RTMPose with a real clip without claiming pose/fall performance.

This is a runtime smoke test only.  It uses a bounded in-memory frame probe;
it does not run a second detector or central retracker and writes aggregates
only.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from datetime import datetime, timezone
from pathlib import Path

import cv2

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "services" / "vision-worker"))
from core.enrichment_v2 import ConfirmedHumanROI  # noqa: E402
from core.enrichment_v3 import VisionEnrichmentPipelineV3  # noqa: E402
from core.rtmpose_backend import RTMPosePoseEnricher  # noqa: E402


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--clip", type=Path, required=True)
    parser.add_argument("--out", type=Path, default=Path("/tmp/synora-rtmpose-clip-smoke.json"))
    parser.add_argument("--max-frames", type=int, default=3)
    args = parser.parse_args()
    result = {"schema_version": "synora.vision.rtmpose-clip-smoke/v1", "clip": str(args.clip), "model_path": os.getenv("SYNORA_RTMPOSE_MODEL_PATH", ""), "runtime": "rknn/rk3588", "pose_status": "unavailable", "latency_ms": "not_measured", "frames_probed": 0, "fall_validation": "not_performed", "physical_action_executed": False, "audio_rendered": False}
    capture = cv2.VideoCapture(str(args.clip))
    if not capture.isOpened():
        result["reason"] = "clip could not be opened"
    else:
        pose = RTMPosePoseEnricher()
        pipeline = VisionEnrichmentPipelineV3(pose=pose)
        started = datetime.now(timezone.utc)
        for index in range(max(0, args.max_frames)):
            ok, frame = capture.read()
            if not ok:
                break
            roi = ConfirmedHumanROI(f"smoke-{index}", True, "protected_interior", "P1_urgent_presence", started, pose_sample={"image": frame})
            aggregate = pipeline.process([roi], real_detection=True, replay_simulation=False)
            result["frames_probed"] += 1
            result["pose_status"] = aggregate.pose_model_status
            result["latency_ms"] = aggregate.pose_latency_ms
            result["signals"] = aggregate.as_bus_payload()["pose"]
            del frame
        capture.release()
        pose.close()
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps(result, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
