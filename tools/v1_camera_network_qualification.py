#!/usr/bin/env python3
"""Deterministic camera/network qualification report.

Fixture results are useful for exercising the protocol, but are never physical
qualification evidence. No quality threshold is inferred by this tool.
"""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
from statistics import mean
from typing import Any

SCHEMA_VERSION = 1
EXPECTED_CAMERA_IDS = ("cam_01", "cam_02", "cam_03")
REQUIRED_DETECTIONS = ("fall", "weapon", "tamper")


def read_json(path: Path) -> dict[str, Any]:
    with path.open(encoding="utf-8") as stream:
        value = json.load(stream)
    if not isinstance(value, dict):
        raise ValueError(f"JSON object required: {path}")
    return value


def atomic_json(path: Path, value: dict[str, Any]) -> None:
    parent_exists = path.parent.exists()
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    if not parent_exists:
        path.parent.chmod(0o700)
    temporary = path.with_name(f".{path.name}.tmp")
    with temporary.open("w", encoding="utf-8") as stream:
        json.dump(value, stream, indent=2, sort_keys=True)
        stream.write("\n")
        stream.flush()
        os.fsync(stream.fileno())
    temporary.chmod(0o600)
    os.replace(temporary, path)
    path.chmod(0o600)


def required_number(value: Any, name: str, *, integer: bool = False) -> float | int:
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        raise ValueError(f"{name} must be numeric")
    if integer and (not isinstance(value, int) or value < 0):
        raise ValueError(f"{name} must be a non-negative integer")
    if value < 0:
        raise ValueError(f"{name} must be non-negative")
    return value


def evaluate_camera(camera: dict[str, Any]) -> dict[str, Any]:
    camera_id = str(camera.get("id", "")).strip()
    if not camera_id:
        raise ValueError("camera id is required")
    phases: dict[str, Any] = {}
    for phase in ("day", "night"):
        entry = camera.get(phase)
        if not isinstance(entry, dict):
            raise ValueError(f"{camera_id}.{phase} is required")
        frames = required_number(entry.get("frames"), f"{camera_id}.{phase}.frames", integer=True)
        usable = required_number(entry.get("usable_frames"), f"{camera_id}.{phase}.usable_frames", integer=True)
        if usable > frames:
            raise ValueError(f"{camera_id}.{phase}.usable_frames exceeds frames")
        phases[phase] = {"frames": frames, "usable_frames": usable}

    ir_cut = camera.get("ir_cut")
    if not isinstance(ir_cut, dict):
        raise ValueError(f"{camera_id}.ir_cut is required")
    transitions = required_number(ir_cut.get("transitions"), f"{camera_id}.ir_cut.transitions", integer=True)
    successful = required_number(ir_cut.get("successful_transitions"), f"{camera_id}.ir_cut.successful_transitions", integer=True)
    if successful > transitions:
        raise ValueError(f"{camera_id}.ir_cut.successful_transitions exceeds transitions")

    upload = camera.get("upload")
    if not isinstance(upload, dict):
        raise ValueError(f"{camera_id}.upload is required")
    sent = required_number(upload.get("sent"), f"{camera_id}.upload.sent", integer=True)
    delivered = required_number(upload.get("delivered"), f"{camera_id}.upload.delivered", integer=True)
    if delivered > sent:
        raise ValueError(f"{camera_id}.upload.delivered exceeds sent")
    latencies = upload.get("latency_ms")
    if not isinstance(latencies, list) or not latencies:
        raise ValueError(f"{camera_id}.upload.latency_ms must be a non-empty list")
    latency_values = [required_number(value, f"{camera_id}.upload.latency_ms") for value in latencies]

    synoranet = camera.get("synoranet")
    if not isinstance(synoranet, dict):
        raise ValueError(f"{camera_id}.synoranet is required")
    attempts = required_number(synoranet.get("reconnect_attempts"), f"{camera_id}.synoranet.reconnect_attempts", integer=True)
    successes = required_number(synoranet.get("reconnect_successes"), f"{camera_id}.synoranet.reconnect_successes", integer=True)
    if successes > attempts:
        raise ValueError(f"{camera_id}.synoranet.reconnect_successes exceeds attempts")

    return {
        "id": camera_id,
        "source": "fixture",
        "day_night": phases,
        "ir_cut": {
            "transitions": transitions,
            "successful_transitions": successful,
            "unqualified_transition_count": transitions - successful,
        },
        "upload": {
            "sent": sent,
            "delivered": delivered,
            "lost": sent - delivered,
            "loss_ratio": (sent - delivered) / sent if sent else None,
            "latency_ms": {"count": len(latency_values), "min": min(latency_values), "max": max(latency_values), "mean": mean(latency_values)},
        },
        "synoranet": {
            "reconnect_attempts": attempts,
            "reconnect_successes": successes,
            "unrecovered": attempts - successes,
        },
    }


def evaluate(document: dict[str, Any]) -> dict[str, Any]:
    if document.get("schema_version") != SCHEMA_VERSION:
        raise ValueError("unsupported fixture schema")
    if document.get("source") != "synthetic_fixture_not_physical_evidence":
        raise ValueError("fixture source must explicitly be non-physical")
    cameras = document.get("cameras")
    if not isinstance(cameras, list):
        raise ValueError("cameras must be a list")
    evaluated = [evaluate_camera(camera) for camera in cameras if isinstance(camera, dict)]
    ids = tuple(item["id"] for item in evaluated)
    required = {
        "three_camera_parallel_set": ids == EXPECTED_CAMERA_IDS,
        "all_camera_measurements_parseable": len(evaluated) == len(cameras) == len(EXPECTED_CAMERA_IDS),
    }
    detections = document.get("detections")
    if not isinstance(detections, dict):
        raise ValueError("detections must be an object")
    detection_decisions = {}
    for name in REQUIRED_DETECTIONS:
        entry = detections.get(name)
        if not isinstance(entry, dict) or not str(entry.get("status", "")).strip():
            raise ValueError(f"detection decision required: {name}")
        detection_decisions[name] = entry
    sensors = document.get("sensors")
    if not isinstance(sensors, dict):
        raise ValueError("sensors must be an object")
    sensor_decisions = {}
    for name in ("pir", "doppler"):
        entry = sensors.get(name)
        if not isinstance(entry, dict) or not str(entry.get("status", "")).strip():
            raise ValueError(f"sensor decision required: {name}")
        sensor_decisions[name] = entry
    audio = document.get("audio")
    if not isinstance(audio, dict) or not isinstance(audio.get("camera_microphone"), dict):
        raise ValueError("camera microphone decision required")
    return {
        "schema_version": SCHEMA_VERSION,
        "source": document["source"],
        "status": "fixture_observed_physical_qualification_blocked",
        "physical_qualification": "blocked_no_target_confirmation",
        "checks": required,
        "cameras": evaluated,
        "sensor_decisions": sensor_decisions,
        "camera_microphone_decision": audio["camera_microphone"],
        "detection_decisions": detection_decisions,
        "limitations": [
            "day/night/IR-cut values are synthetic fixture observations",
            "upload loss and latency are reported, not threshold-qualified",
            "PIR, Doppler, camera microphone and detection capabilities require target hardware evidence",
        ],
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--fixture", type=Path, required=True)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    report = evaluate(read_json(args.fixture))
    rendered = json.dumps(report, indent=2, sort_keys=True) + "\n"
    if args.output:
        atomic_json(args.output, report)
    else:
        print(rendered, end="")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
