#!/usr/bin/env python3
"""Reproducible RTMPose-s acquisition/export/conversion diagnostics.

All generated weights stay below build/.  The command never installs a model
into /opt or /var/lib and never treats a missing conversion tool as success.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import urllib.request
import zipfile


ROOT = Path(__file__).resolve().parents[1]
MANIFEST_PATH = ROOT / "configs" / "rtmpose-s.manifest.json"
ARCHIVE_URL = "https://download.openmmlab.com/mmpose/v1/projects/rtmposev1/onnx_sdk/rtmpose-s_simcc-body7_pt-body7_420e-256x192-acd4a1ef_20230504.zip"
ARCHIVE_SHA256 = "7673922e531014906ca4f0f239b7e233b740146a10b632deaa2a28d45470d802"


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def source_dir(work_dir: Path) -> Path:
    matches = list((work_dir / "source").glob("**/end2end.onnx"))
    return matches[0].parent if matches else work_dir / "source" / "official"


def download(args: argparse.Namespace) -> int:
    target = args.work_dir / "source" / "rtmpose-s-official.zip"
    target.parent.mkdir(parents=True, exist_ok=True)
    if not target.is_file():
        urllib.request.urlretrieve(ARCHIVE_URL, target)
    actual = sha256(target)
    if actual != ARCHIVE_SHA256:
        raise SystemExit(f"archive sha256 mismatch: {actual} != {ARCHIVE_SHA256}")
    extract = target.parent / "extracted"
    extract.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(target) as archive:
        archive.extractall(extract)
    print(json.dumps({"archive": str(target), "sha256": actual, "extracted": str(extract)}, sort_keys=True))
    return 0


def export_onnx(args: argparse.Namespace) -> int:
    work = args.work_dir
    mmdeploy = os.getenv("SYNORA_MMDEPLOY_ROOT", "")
    mmpose = os.getenv("SYNORA_MMPOSE_ROOT", "")
    image = os.getenv("SYNORA_RTMPOSE_TEST_IMAGE", "")
    if not mmdeploy or not mmpose or not image:
        print(json.dumps({"status": "blocked", "reason": "SYNORA_MMDEPLOY_ROOT, SYNORA_MMPOSE_ROOT and SYNORA_RTMPOSE_TEST_IMAGE are required", "output": str(work / "onnx" / "end2end_static.onnx")}, sort_keys=True))
        return 2
    command = [sys.executable, str(Path(mmdeploy) / "tools" / "deploy.py"), str(Path(mmdeploy) / "configs/mmpose/pose-detection_simcc_onnxruntime_static-256x192.py"), str(Path(mmpose) / "configs/body_2d_keypoint/rtmpose/coco/rtmpose-s_8xb256-420e_coco-256x192.py"), os.getenv("SYNORA_RTMPOSE_CHECKPOINT", "https://download.openmmlab.com/mmpose/v1/projects/rtmposev1/rtmpose-s_simcc-body7_pt-body7_420e-256x192-acd4a1ef_20230504.pth"), image, "--work-dir", str(work / "onnx"), "--device", "cpu", "--dump-info"]
    print(" ".join(command))
    return subprocess.call(command)


def convert_rknn(args: argparse.Namespace) -> int:
    work = args.work_dir
    onnx = work / "onnx" / "end2end_static.onnx"
    output = work / "rknn" / "rtmpose-s.rknn"
    if not onnx.is_file():
        print(json.dumps({"status": "blocked", "reason": "static ONNX is unavailable; dynamic official archive is not accepted for this conversion", "onnx": str(onnx), "rknn": str(output)}, sort_keys=True))
        return 2
    try:
        from rknn.api import RKNN  # type: ignore
    except Exception as exc:
        print(json.dumps({"status": "blocked", "reason": f"RKNN Toolkit is unavailable: {exc}", "config": "pose-detection_simcc_rknn-fp16_static-256x192.py", "target": "rk3588"}, sort_keys=True))
        return 2
    output.parent.mkdir(parents=True, exist_ok=True)
    rknn = RKNN(verbose=False)
    try:
        if rknn.config(target_platform="rk3588", optimization_level=1) != 0:
            raise RuntimeError("RKNN config failed")
        if rknn.load_onnx(model=str(onnx)) != 0:
            raise RuntimeError("RKNN ONNX import failed")
        if rknn.build(do_quantization=False) != 0:
            raise RuntimeError("RKNN build failed")
        if rknn.export_rknn(str(output)) != 0:
            raise RuntimeError("RKNN export failed")
    finally:
        rknn.release()
    print(json.dumps({"status": "available", "rknn": str(output), "target": "rk3588", "quantization": "disabled"}, sort_keys=True))
    return 0


def verify_runtime(args: argparse.Namespace) -> int:
    model_value = os.getenv("SYNORA_RTMPOSE_MODEL_PATH", "").strip()
    model = Path(model_value) if model_value else None
    result = {"status": "unavailable", "model_path": model_value, "backend": "rknn", "target": "rk3588", "latency_ms": "not_measured"}
    if model is None or not model.is_file():
        result["reason"] = "SYNORA_RTMPOSE_MODEL_PATH is missing or not a file"
        print(json.dumps(result, sort_keys=True))
        return 2
    try:
        sys.path.insert(0, str(ROOT / "services" / "vision-worker"))
        from core.rtmpose_backend import RTMPoseBackend
        backend = RTMPoseBackend(str(model))
        result.update(backend.capability())
        backend.close()
        print(json.dumps(result, sort_keys=True))
        return 0 if result.get("status") == "available" else 2
    except Exception as exc:
        result["reason"] = str(exc)
        print(json.dumps(result, sort_keys=True))
        return 2


def manifest(args: argparse.Namespace) -> int:
    base = json.loads(MANIFEST_PATH.read_text(encoding="utf-8"))
    work = args.work_dir
    files = {}
    for path in (work / "source").glob("**/*"):
        if path.is_file() and path.suffix in {".zip", ".pth", ".onnx", ".rknn"}:
            files[str(path.relative_to(work))] = {"sha256": sha256(path), "bytes": path.stat().st_size}
    model_value = os.getenv("SYNORA_RTMPOSE_MODEL_PATH", "").strip()
    model = Path(model_value) if model_value else None
    base["generated"] = {"work_dir": str(work), "artifacts": files, "rknn_runtime": {"status": "not_run" if model is None or not model.is_file() else "requested", "model_path": model_value}}
    output = work / "RTMPOSE-S.MANIFEST.json"
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(base, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(output)
    return 0


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--work-dir", type=Path, default=ROOT / "build" / "rtmpose-s")
    sub = parser.add_subparsers(dest="command", required=True)
    for name, func in (("download", download), ("export-onnx", export_onnx), ("convert-rknn", convert_rknn), ("verify-runtime", verify_runtime), ("manifest", manifest)):
        command = sub.add_parser(name); command.set_defaults(func=func)
    args = parser.parse_args()
    return args.func(args)


if __name__ == "__main__":
    raise SystemExit(main())
