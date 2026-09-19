#!/usr/bin/env python3
"""Close a local video into deterministic segment-ready inputs for the V1 harness."""

from __future__ import annotations

import argparse
import hashlib
import json
from datetime import datetime, timedelta, timezone
import os
from pathlib import Path
import subprocess
import sys
import tempfile

import cv2


TOPOLOGIES = {"public_outdoor", "private_perimeter", "restricted_threshold", "protected_interior", "unknown"}


def segment_id(camera_id: str, episode_id: str, index: int, digest: str) -> str:
    return "segment-" + hashlib.sha256(f"{camera_id}|{episode_id}|{index}|{digest.lower()}".encode()).hexdigest()[:24]


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--clip", required=True)
    parser.add_argument("--segment-seconds", type=float, required=True)
    parser.add_argument("--camera-id", required=True)
    parser.add_argument("--node-id", required=True)
    parser.add_argument("--zone", required=True)
    parser.add_argument("--trigger", required=True)
    parser.add_argument("--out", required=True)
    return parser.parse_args()


def fail(message: str) -> int:
    print(f"SEGMENT_REPLAY_VALIDATION_FAILED: {message}", file=sys.stderr)
    return 2


def main() -> int:
    args = parse_args()
    source = Path(args.clip).expanduser().resolve()
    if not source.is_file():
        return fail(f"clip is not a regular file: {source}")
    if args.segment_seconds < 0.25 or args.segment_seconds > 2.0:
        return fail("segment duration must be between 0.25 and 2 seconds")
    if args.zone not in TOPOLOGIES:
        return fail(f"zone must be a supported topology class: {args.zone}")

    capture = cv2.VideoCapture(str(source))
    if not capture.isOpened():
        return fail(f"clip could not be opened: {source}")
    fps = float(capture.get(cv2.CAP_PROP_FPS) or 0.0)
    frame_count = int(capture.get(cv2.CAP_PROP_FRAME_COUNT) or 0)
    width = int(capture.get(cv2.CAP_PROP_FRAME_WIDTH) or 0)
    height = int(capture.get(cv2.CAP_PROP_FRAME_HEIGHT) or 0)
    if fps <= 0 or frame_count <= 0 or width <= 0 or height <= 0:
        capture.release()
        return fail("clip must expose positive FPS, frame count and dimensions")

    frames_per_segment = max(1, int(round(args.segment_seconds * fps)))
    episode_id = f"episode-segment-replay-{source.stem}"
    output = Path(args.out).expanduser().resolve()
    output.mkdir(parents=True, exist_ok=True)
    segments_dir = Path(tempfile.mkdtemp(prefix="segments-", dir=str(output)))
    manifest: list[dict[str, object]] = []
    fixed_start = datetime(2026, 1, 1, tzinfo=timezone.utc)
    index = 0
    frames: list[object] = []

    def close_segment(items: list[object], segment_index: int, final: bool) -> None:
        if not items:
            return
        filename = f"segment-{segment_index:04d}.mp4"
        path = segments_dir / filename
        writer = cv2.VideoWriter(str(path), cv2.VideoWriter_fourcc(*"mp4v"), fps, (width, height))
        if not writer.isOpened():
            raise RuntimeError("could not create segment writer")
        try:
            for frame in items:
                writer.write(frame)
        finally:
            writer.release()
        digest = hashlib.sha256(path.read_bytes()).hexdigest()
        start = fixed_start + timedelta(seconds=segment_index * frames_per_segment / fps)
        end = start + timedelta(seconds=len(items) / fps)
        # OpenCV rounding can make a final short tail; reject it rather than
        # emitting a segment that the contract would refuse.
        if (end - start).total_seconds() < 0.25:
            raise RuntimeError("final segment is shorter than the contract minimum")
        manifest.append({
            "path": str(path),
            "segment": {
                "schema_version": "synora.vision.segment-ready/v1",
                "camera_id": args.camera_id,
                "node_id": args.node_id,
                "episode_id": episode_id,
                "segment_id": segment_id(args.camera_id, episode_id, segment_index, digest),
                "segment_index": segment_index,
                "started_at": start.isoformat().replace("+00:00", "Z"),
                "ended_at": end.isoformat().replace("+00:00", "Z"),
                "is_final": final,
                "topology_class": args.zone,
                "trigger": args.trigger,
                "media_ref": f"local://segment-replay/{filename}",
                "content_sha256": digest,
            },
        })

    try:
        while True:
            ok, frame = capture.read()
            if not ok:
                break
            frames.append(frame)
            if len(frames) >= frames_per_segment:
                close_segment(frames, index, False)
                frames = []
                index += 1
        if frames:
            # A tail after complete windows is the only final segment.
            close_segment(frames, index, True)
        elif manifest:
            manifest[-1]["segment"]["is_final"] = True  # type: ignore[index]
    except (OSError, RuntimeError, ValueError) as exc:
        return fail(str(exc))
    finally:
        capture.release()

    if not manifest:
        return fail("clip produced no segments")
    manifest[-1]["segment"]["is_final"] = True  # type: ignore[index]
    manifest_path = output / "manifest.json"
    manifest_path.write_text(json.dumps({"source": str(source), "fps": fps, "segments": manifest}, indent=2) + "\n", encoding="utf-8")

    if os.getenv("SYNORA_REAL_VISION", "0") == "1":
        real_command = [
            sys.executable,
            str(Path(__file__).with_name("replay_real_v1.py")),
            "--clip", str(source),
            "--manifest", str(manifest_path),
            "--out", str(output),
            "--camera-id", args.camera_id,
            "--node-id", args.node_id,
            "--zone", args.zone,
            "--trigger", args.trigger,
        ]
        result = subprocess.run(real_command, cwd=Path(__file__).resolve().parents[2], check=False)
        return result.returncode

    # Compatibility path for contract-only fixtures. Production E2E sets
    # SYNORA_REAL_VISION=1 and refuses this simulated observation path.
    observations_path = output / "observations.jsonl"
    with observations_path.open("w", encoding="utf-8") as stream:
        for index, item in enumerate(manifest):
            segment = item["segment"]
            priority = "P1_urgent_presence" if args.zone in {"restricted_threshold", "protected_interior"} else "P2_contextual_enrichment"
            state = "candidate" if index == 0 else "confirmed"
            payload = {
                "schema_version": "synora.vision.observation-replay/v1",
                "episode_id": segment["episode_id"],
                "topology": segment["topology_class"],
                "priority": priority,
                "episode_phase": "final" if segment["is_final"] else state,
                "human_present": True,
                "track_count": 1,
                "track_confirmed": index > 0,
                "segment_count": index + 1,
                "observation_count": index + 1,
                "confidence": 0.5,
                "real_detection": False,
                "replay_simulation": True,
                "movement": True,
                "sensor_evidence": True,
            }
            stream.write(json.dumps({
                "type": "synora.vision.clip-observation/v1",
                "episode_id": segment["episode_id"],
                "topology_class": segment["topology_class"],
                "trigger": args.trigger,
                "observed_at": segment["ended_at"],
                "sequence": index + 1,
                "priority_hint": priority,
                "priority_state": state,
                "reason_codes": ["segment_replay", "human_presence_aggregate"],
                "tracks": [{"track_id": "aggregate-track", "subject_type": "human", "confidence": 0.5, "state": state, "detection_count": 1}],
                "backend": {"status": "replay", "real_model": False},
                "payload": payload,
            }, sort_keys=True) + "\n")
    (output / "summary.contract.json").write_text(json.dumps({
        "schema_version": "synora.vision.summary-replay/v1",
        "episode_id": manifest[0]["segment"]["episode_id"],
        "observed_at": manifest[-1]["segment"]["ended_at"],
        "summary_count": 1,
        "physical_action_executed": False,
    }, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"segment_simulation": True, "source": str(source), "segments": len(manifest), "manifest": str(manifest_path)}, sort_keys=True))
    project_root = Path(__file__).resolve().parents[2]
    go_binary = os.environ.get("GO") or os.environ.get("SYNORA_GO") or shutil_which("go")
    if not go_binary:
        return fail("Go compiler not found")
    env = os.environ.copy()
    env["SYNORA_SEGMENT_MANIFEST"] = str(manifest_path)
    env["SYNORA_REPLAY_OUT"] = str(output)
    command = [go_binary, "run", "./cmd/synora-v1-replay", "--observations", str(observations_path), "--out", str(output / "core-replay.json")]
    result = subprocess.run(command, cwd=project_root, env=env, check=False)
    if result.returncode == 0:
        print(f"SEGMENT_REPLAY_OUTPUT_DIR {output}")
    return result.returncode


def shutil_which(command: str) -> str | None:
    for directory in os.environ.get("PATH", "").split(os.pathsep):
        candidate = Path(directory) / command
        if candidate.is_file() and os.access(candidate, os.X_OK):
            return str(candidate)
    return None


if __name__ == "__main__":
    raise SystemExit(main())
