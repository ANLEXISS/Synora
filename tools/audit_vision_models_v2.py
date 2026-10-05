#!/usr/bin/env python3
"""Audit Vision V2 model artifacts without promoting or fabricating metrics."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import sys
from typing import Any


def file_record(path: Path) -> dict[str, Any]:
    if not path.is_file():
        return {"path": str(path), "status": "missing"}
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return {"path": str(path), "status": "present", "bytes": path.stat().st_size, "sha256": digest.hexdigest()}


def first_existing(paths: list[Path]) -> dict[str, Any]:
    for path in paths:
        if path.is_file():
            return file_record(path)
    return {"status": "missing", "expected_paths": [str(path) for path in paths]}


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", default=str(Path(__file__).resolve().parents[1]))
    parser.add_argument("--pre-git", default="/home/rock/Synora.pre-git-20260721T173625Z")
    parser.add_argument("--out", default="/tmp/synora-vision-model-audit-v2.json")
    args = parser.parse_args()

    repo = Path(args.repo).resolve()
    pre_git = Path(args.pre_git).resolve()
    model_root = repo / "models"
    conversion_root = repo / "tools" / "model-conversion"
    pre_conversion_root = pre_git / "tools" / "model-conversion"

    pose_paths = [
        model_root / "yolov8n-pose.rknn",
        conversion_root / "yolov8n-pose.rknn",
        conversion_root / "yolov8n-pose.onnx",
        pre_git / "models" / "yolov8n-pose.rknn",
        pre_conversion_root / "yolov8n-pose.rknn",
        pre_conversion_root / "yolov8n-pose.onnx",
    ]
    weapon_paths = [
        conversion_root / "weapon.onnx",
        pre_conversion_root / "weapon.onnx",
    ]
    weapon_rknn_paths = [model_root / "weapon.rknn", pre_git / "models" / "weapon.rknn", pre_conversion_root / "weapon.rknn"]

    onnx_available = shutil.which("python3") is not None
    try:
        import onnx  # type: ignore  # noqa: F401
    except Exception:
        onnx_available = False

    pose = first_existing(pose_paths)
    weapon = first_existing(weapon_paths)
    weapon_rknn = first_existing(weapon_rknn_paths)
    if weapon.get("status") == "present":
        weapon.update({
            "qualification": "not_qualified",
            "promoted": False,
            "onnx_python_validator_available": onnx_available,
            "onnx_valid": "not_verified" if not onnx_available else "not_run",
            "provenance": "unknown",
            "license": "not_found",
            "classes": "unknown",
            "preprocess": "unknown",
            "postprocess": "unknown",
            "rknn_parity": "not_measured",
            "real_metrics": "absent",
        })

    inventory = {}
    for name in ("arcface_w600k_r50.rknn", "det_10g.rknn", "face_detection_yunet_2023mar.rknn", "yolov8.rknn"):
        inventory[name] = first_existing([model_root / name, pre_git / "models" / name])

    report = {
        "schema_version": "synora.vision-model-audit/v2",
        "repo": str(repo),
        "pre_git_reference": str(pre_git),
        "integrated_models": [],
        "audited_models": {
            "yolov8n-pose": {
                **pose,
                "role": "central_pose_candidate",
                "qualification": "not_available" if pose.get("status") == "missing" else "not_qualified",
                "promoted": False,
                "conversion_onnx_to_rknn": "not_verified",
                "input_dimension": "not_verified",
                "postprocess": "not_verified",
                "onnx_rknn_parity": "not_measured",
                "rk3588_memory": "not_measured",
                "rk3588_latency_ms": "not_measured",
            },
            "weapon": {"onnx": weapon, "rknn": weapon_rknn, "promoted": False},
        },
        "existing_runtime_inventory": inventory,
        "rk3588": {
            "target": "rk3588",
            "pose_memory_measurement": "not_run",
            "pose_latency_measurement_ms": "not_run",
            "reason": "no qualified yolov8n-pose RKNN artifact is present; no pose benchmark was executed",
        },
        "conclusion": "No new Vision model is promoted. V2 uses aggregate adapters and remains a candidate path; V1 remains nominal.",
    }
    output = Path(args.out)
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(output)
    return 0


if __name__ == "__main__":
    sys.exit(main())
