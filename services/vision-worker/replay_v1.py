#!/usr/bin/env python3
"""Maintained real-clip replay entrypoint for Vision Clip V1 dry-run mode."""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
from typing import Any
import shutil

import cv2


ALLOWED_EXTENSIONS = {".mp4", ".mov", ".mkv", ".avi", ".webm", ".m4v"}


def inspect_clip(path: Path, max_duration_seconds: float) -> dict[str, Any]:
    if not path.is_file():
        raise ValueError(f"clip is not a regular file: {path}")
    if path.suffix.lower() not in ALLOWED_EXTENSIONS:
        raise ValueError(f"unsupported clip format: {path.suffix or '<none>'}")
    capture = cv2.VideoCapture(str(path))
    try:
        if not capture.isOpened():
            raise ValueError(f"clip could not be opened: {path}")
        fps = float(capture.get(cv2.CAP_PROP_FPS) or 0.0)
        frame_count = int(capture.get(cv2.CAP_PROP_FRAME_COUNT) or 0)
        if fps <= 0.0 or frame_count <= 0:
            raise ValueError("clip must expose a positive FPS and frame count")
        duration = frame_count / fps
        if duration <= 0.0 or duration > max_duration_seconds:
            raise ValueError(
                f"clip duration {duration:.3f}s exceeds limit {max_duration_seconds:.3f}s"
            )
        return {"path": str(path), "fps": fps, "frames": frame_count, "duration_seconds": duration}
    finally:
        capture.release()


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--clip", required=True)
    parser.add_argument("--camera-id", required=True)
    parser.add_argument("--node-id", required=True)
    parser.add_argument("--zone", required=True)
    parser.add_argument("--trigger", required=True)
    parser.add_argument("--expect")
    parser.add_argument("--out")
    return parser


def main(argv: list[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    try:
        max_duration = float(os.getenv("SYNORA_VISION_V1_MAX_DURATION", "10"))
        if max_duration <= 0:
            raise ValueError("SYNORA_VISION_V1_MAX_DURATION must be positive")
        metadata = inspect_clip(Path(args.clip).expanduser().resolve(), max_duration)
        expectation = None
        if args.expect:
            expectation_path = Path(args.expect).expanduser().resolve()
            if not expectation_path.is_file():
                raise ValueError(f"expectation file is not a regular file: {expectation_path}")
            expectation = json.loads(expectation_path.read_text(encoding="utf-8"))
            if not isinstance(expectation, dict):
                raise ValueError("expectation must be a JSON object")
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        print(f"REPLAY_VALIDATION_FAILED: {exc}", file=sys.stderr)
        return 2

    project_root = Path(__file__).resolve().parents[2]
    output_dir = Path(args.out or os.getenv("SYNORA_REPLAY_OUT") or tempfile.mkdtemp(prefix="synora-vision-replay-"))
    output_dir.mkdir(parents=True, exist_ok=True)
    env = os.environ.copy()
    env.update({
        "SYNORA_REPLAY_CLIP": str(Path(metadata["path"])),
        "SYNORA_REPLAY_CAMERA_ID": args.camera_id,
        "SYNORA_REPLAY_NODE_ID": args.node_id,
        "SYNORA_REPLAY_ZONE": args.zone,
        "SYNORA_REPLAY_TRIGGER": args.trigger,
        "SYNORA_REPLAY_OUT": str(output_dir.resolve()),
    })
    if args.expect:
        env["SYNORA_REPLAY_EXPECT"] = str(Path(args.expect).expanduser().resolve())

    print(json.dumps({"validated_clip": metadata, "output_dir": str(output_dir.resolve())}, sort_keys=True))
    go_binary = os.environ.get("GO") or os.environ.get("SYNORA_GO") or shutil.which("go")
    if not go_binary:
        print("REPLAY_VALIDATION_FAILED: Go compiler not found; set GO=/path/to/go", file=sys.stderr)
        return 2
    command = [go_binary, "test", "./cmd/synora-core", "-run", "^TestV1RealClipReplay$", "-count=1", "-v"]
    result = subprocess.run(command, cwd=project_root, env=env, check=False)
    if result.returncode == 0:
        print(f"REPLAY_OUTPUT_DIR {output_dir.resolve()}")
    return result.returncode


if __name__ == "__main__":
    raise SystemExit(main())
